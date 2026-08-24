package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	serverName     = "synology-mcpserver"
	serverVersion  = "0.1.2"
	defaultPkgDir  = "/var/packages"
	defaultMaxLogs = 200
)

var supportedVersions = []string{
	"2026-07-28",
	"2025-11-25",
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

type config struct {
	PackagesDir      string
	SynopkgBin       string
	SynosystemctlBin string
	JournalctlBin    string
	MaxJournalLen    int
	DownloadLimit    time.Duration
	AllowUninstall   bool
}

func defaultConfig() config {
	return config{
		PackagesDir:      envOrDefault("MCP_SYNO_PACKAGES_DIR", defaultPkgDir),
		SynopkgBin:       envOrDefault("MCP_SYNO_SYNOPKG_BIN", "synopkg"),
		SynosystemctlBin: envOrDefault("MCP_SYNO_SYNOSYSTEMCTL_BIN", "synosystemctl"),
		JournalctlBin:    envOrDefault("MCP_SYNO_JOURNALCTL_BIN", "journalctl"),
		MaxJournalLen:    envIntOrDefault("MCP_SYNO_JOURNAL_LINES", defaultMaxLogs),
		DownloadLimit:    envDurationOrDefault("MCP_SYNO_DOWNLOAD_TIMEOUT", 2*time.Minute),
		AllowUninstall:   envBoolOrDefault("MCP_SYNO_ALLOW_UNINSTALL", true),
	}
}

type server struct {
	cfg     config
	metrics *metricsCollector
	mu      sync.Mutex
	enc     *json.Encoder
	out     io.Writer
}

func newServer(cfg config) *server {
	return &server{cfg: cfg, metrics: newMetricsCollector()}
}

func (s *server) Run(ctx context.Context, in io.Reader, out io.Writer, errOut io.Writer) error {
	s.out = out
	s.enc = json.NewEncoder(out)
	s.enc.SetEscapeHTML(false)

	dec := json.NewDecoder(bufio.NewReader(in))
	dec.UseNumber()

	for {
		var req rpcRequest
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if req.Method == "" {
			continue
		}

		if len(req.ID) == 0 {
			continue
		}

		resp := s.handle(ctx, req)
		s.mu.Lock()
		err := s.enc.Encode(resp)
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type discoverResult struct {
	ResultType        string         `json:"resultType"`
	SupportedVersions []string       `json:"supportedVersions"`
	Capabilities      serverCaps     `json:"capabilities"`
	ServerInfo        implementation `json:"serverInfo"`
	Instructions      string         `json:"instructions,omitempty"`
}

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    serverCaps     `json:"capabilities"`
	ServerInfo      implementation `json:"serverInfo"`
	Instructions    string         `json:"instructions,omitempty"`
}

type serverCaps struct {
	Tools *toolCaps `json:"tools,omitempty"`
}

type toolCaps struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type listToolsResult struct {
	Tools []tool `json:"tools"`
}

type tool struct {
	Name         string           `json:"name"`
	Title        string           `json:"title,omitempty"`
	Description  string           `json:"description,omitempty"`
	InputSchema  map[string]any   `json:"inputSchema"`
	OutputSchema map[string]any   `json:"outputSchema,omitempty"`
	Annotations  *toolAnnotations `json:"annotations,omitempty"`
}

type toolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint bool   `json:"destructiveHint,omitempty"`
	IdempotentHint  bool   `json:"idempotentHint,omitempty"`
	OpenWorldHint   bool   `json:"openWorldHint,omitempty"`
}

type callToolResult struct {
	Content           []content `json:"content"`
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type packageInfo struct {
	Name         string            `json:"name"`
	DisplayName  string            `json:"displayName,omitempty"`
	Version      string            `json:"version,omitempty"`
	Description  string            `json:"description,omitempty"`
	InstallPath  string            `json:"installPath,omitempty"`
	CanUninstall bool              `json:"canUninstall"`
	Info         map[string]string `json:"info"`
}

type packageListResult struct {
	Packages []packageInfo `json:"packages"`
}

type journalSearchResult struct {
	Entries []map[string]any `json:"entries"`
	Count   int              `json:"count"`
}

type serviceHealthResult struct {
	Name         string        `json:"name"`
	Healthy      bool          `json:"healthy"`
	ActiveStatus string        `json:"activeStatus,omitempty"`
	Command      commandResult `json:"command"`
}

type servicePidResult struct {
	Service          string        `json:"service"`
	Healthy          bool          `json:"healthy"`
	ActiveStatus     string        `json:"activeStatus,omitempty"`
	Pid              int           `json:"pid,omitempty"`
	PreviousPid      int           `json:"previousPid,omitempty"`
	PreviousPidAlive bool          `json:"previousPidAlive,omitempty"`
	Command          commandResult `json:"command"`
}

type portHealthResult struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Address string `json:"address"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

type runtimeHealthResult struct {
	Host     string                `json:"host"`
	Healthy  bool                  `json:"healthy"`
	Services []serviceHealthResult `json:"services"`
	Ports    []portHealthResult    `json:"ports"`
}

type commandResult struct {
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	ExitCode int      `json:"exitCode"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
}

type packageCommandResult struct {
	Action           string        `json:"action"`
	Package          string        `json:"package,omitempty"`
	Source           string        `json:"source,omitempty"`
	TempFile         string        `json:"tempFile,omitempty"`
	ChecksumSHA256   string        `json:"checksumSha256,omitempty"`
	ChecksumVerified bool          `json:"checksumVerified,omitempty"`
	Command          commandResult `json:"command"`
	ParsedJSON       any           `json:"parsedJson,omitempty"`
}

type spkUploadResult struct {
	Action         string `json:"action"`
	Source         string `json:"source"`
	TempFile       string `json:"tempFile"`
	ChecksumSHA256 string `json:"checksumSha256"`
	SizeBytes      int64  `json:"sizeBytes"`
}

func (s *server) handle(ctx context.Context, req rpcRequest) (resp rpcResponse) {
	defer func() {
		if s.metrics != nil {
			s.metrics.observeRPC(req.Method, resp.Error == nil)
		}
	}()

	switch req.Method {
	case "server/discover":
		resp = rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: discoverResult{
			ResultType:        "complete",
			SupportedVersions: supportedVersions,
			Capabilities:      s.capabilities(),
			ServerInfo:        implementation{Name: serverName, Version: serverVersion},
			Instructions:      serverInstructions(),
		}}
	case "initialize":
		resp = s.handleInitialize(req)
	case "notifications/initialized":
		resp = rpcResponse{}
	case "tools/list":
		resp = rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: listToolsResult{Tools: s.tools()}}
	case "tools/call":
		resp = s.handleToolCall(ctx, req)
	case "ping":
		resp = rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	default:
		resp = rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found"}}
	}
	return resp
}

func (s *server) handleInitialize(req rpcRequest) rpcResponse {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(req.Params, &params)

	version := negotiateVersion(params.ProtocolVersion)
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: initializeResult{
		ProtocolVersion: version,
		Capabilities:    s.capabilities(),
		ServerInfo:      implementation{Name: serverName, Version: serverVersion},
		Instructions:    serverInstructions(),
	}}
}

func (s *server) capabilities() serverCaps {
	return serverCaps{Tools: &toolCaps{}}
}

func serverInstructions() string {
	return "Use list_packages and package_info to inspect installed SPKs. install_spk accepts a base64 payload, local path, or URL and can verify a SHA-256 digest before install. The HTTP upload endpoint accepts raw SPK bytes and returns the checksum plus temp file path for follow-up install_spk calls. check_runtime verifies Synology service state and TCP listeners. service_pid reports a service PID and can confirm a previous PID disappeared after restart. remove_package refuses packages whose INFO file disables uninstall."
}

func negotiateVersion(requested string) string {
	for _, v := range supportedVersions {
		if v == requested {
			return v
		}
	}
	return supportedVersions[0]
}

func (s *server) tools() []tool {
	return []tool{
		{
			Name:        "list_packages",
			Title:       "List installed packages",
			Description: "List installed packages discovered from the Synology package directory and parsed INFO metadata.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prefix": map[string]any{"type": "string", "description": "Optional package name prefix filter"},
				},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"packages": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "object"},
					},
				},
			},
			Annotations: &toolAnnotations{ReadOnlyHint: true},
		},
		{
			Name:        "package_info",
			Title:       "Read package info",
			Description: "Read and return the INFO metadata for one installed package.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package": map[string]any{"type": "string"},
				},
				"required": []string{"package"},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package": map[string]any{"type": "object"},
				},
				"required": []string{"package"},
			},
			Annotations: &toolAnnotations{ReadOnlyHint: true},
		},
		{
			Name:        "search_journal",
			Title:       "Search journal logs",
			Description: "Query journalctl output and return structured JSON entries.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"unit":  map[string]any{"type": "string"},
					"grep":  map[string]any{"type": "string"},
					"since": map[string]any{"type": "string"},
					"lines": map[string]any{"type": "integer", "minimum": 1},
				},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entries": map[string]any{"type": "array"},
					"count":   map[string]any{"type": "integer"},
				},
			},
			Annotations: &toolAnnotations{ReadOnlyHint: true},
		},
		{
			Name:        "install_spk",
			Title:       "Install SPK",
			Description: "Install an SPK from a local path, uploaded base64 payload, or URL. An optional SHA-256 digest can be supplied and verified before install. The response is a JSON execution record.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"spk_path":   map[string]any{"type": "string"},
					"spk_base64": map[string]any{"type": "string"},
					"spk_url":    map[string]any{"type": "string"},
					"spk_sha256": map[string]any{"type": "string", "description": "Optional SHA-256 digest of the SPK, with or without a sha256: prefix"},
				},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":           map[string]any{"type": "string"},
					"command":          map[string]any{"type": "object"},
					"checksumSha256":   map[string]any{"type": "string"},
					"checksumVerified": map[string]any{"type": "boolean"},
				},
			},
			Annotations: &toolAnnotations{DestructiveHint: true, OpenWorldHint: true},
		},
		{
			Name:        "check_runtime",
			Title:       "Check runtime health",
			Description: "Verify Synology service state and TCP listeners for a set of services and ports.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host":       map[string]any{"type": "string", "description": "Host to probe for port checks, defaults to 127.0.0.1"},
					"timeout_ms": map[string]any{"type": "integer", "minimum": 1, "description": "TCP probe timeout in milliseconds"},
					"services": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Synology service unit names to inspect with synosystemctl get-active-status",
					},
					"ports": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "integer", "minimum": 1},
						"description": "TCP ports to probe on the host",
					},
				},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host":     map[string]any{"type": "string"},
					"healthy":  map[string]any{"type": "boolean"},
					"services": map[string]any{"type": "array"},
					"ports":    map[string]any{"type": "array"},
				},
			},
			Annotations: &toolAnnotations{ReadOnlyHint: true},
		},
		{
			Name:        "service_pid",
			Title:       "Read service PID",
			Description: "Read the PID for a Synology service and optionally confirm that a previous PID is gone after restart.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service": map[string]any{"type": "string", "description": "Synology service unit name"},
					"previous_pid": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional PID that should no longer be running after an update",
					},
				},
				"required": []string{"service"},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service":          map[string]any{"type": "string"},
					"healthy":          map[string]any{"type": "boolean"},
					"activeStatus":     map[string]any{"type": "string"},
					"pid":              map[string]any{"type": "integer"},
					"previousPid":      map[string]any{"type": "integer"},
					"previousPidAlive": map[string]any{"type": "boolean"},
					"command":          map[string]any{"type": "object"},
				},
				"required": []string{"service", "healthy", "command"},
			},
			Annotations: &toolAnnotations{ReadOnlyHint: true},
		},
		{
			Name:        "remove_package",
			Title:       "Remove package",
			Description: "Remove a non-system package unless its INFO metadata disables uninstall.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package": map[string]any{"type": "string"},
				},
				"required": []string{"package"},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":  map[string]any{"type": "string"},
					"command": map[string]any{"type": "object"},
				},
			},
			Annotations: &toolAnnotations{DestructiveHint: true},
		},
	}
}

func (s *server) handleToolCall(ctx context.Context, req rpcRequest) rpcResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}}
	}

	record := func(tool string, result any, err error) rpcResponse {
		if s.metrics != nil {
			s.metrics.observeTool(tool, err == nil)
		}
		return s.toolResponse(req.ID, result, err)
	}

	switch params.Name {
	case "list_packages":
		res, err := s.listPackages(params.Arguments)
		return record("list_packages", res, err)
	case "package_info":
		res, err := s.packageInfo(params.Arguments)
		return record("package_info", res, err)
	case "search_journal":
		res, err := s.searchJournal(ctx, params.Arguments)
		return record("search_journal", res, err)
	case "install_spk":
		res, err := s.installSPK(ctx, params.Arguments)
		return record("install_spk", res, err)
	case "check_runtime":
		res, err := s.checkRuntime(ctx, params.Arguments)
		return record("check_runtime", res, err)
	case "service_pid":
		res, err := s.servicePID(ctx, params.Arguments)
		return record("service_pid", res, err)
	case "remove_package":
		res, err := s.removePackage(ctx, params.Arguments)
		return record("remove_package", res, err)
	default:
		if s.metrics != nil {
			s.metrics.observeTool(params.Name, false)
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "unknown tool"}}
	}
}

func (s *server) toolResponse(id json.RawMessage, result any, err error) rpcResponse {
	if err != nil {
		payload, _ := json.Marshal(map[string]any{"error": err.Error()})
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: callToolResult{
				Content:           []content{{Type: "text", Text: string(payload)}},
				StructuredContent: map[string]any{"error": err.Error()},
				IsError:           true,
			},
		}
	}
	payload, _ := json.Marshal(result)
	return rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: callToolResult{
			Content:           []content{{Type: "text", Text: string(payload)}},
			StructuredContent: result,
		},
	}
}

func (s *server) listPackages(args json.RawMessage) (packageListResult, error) {
	var input struct {
		Prefix string `json:"prefix"`
	}
	_ = json.Unmarshal(args, &input)

	entries, err := os.ReadDir(s.cfg.PackagesDir)
	if err != nil {
		return packageListResult{}, err
	}

	var packages []packageInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		infoPath := filepath.Join(s.cfg.PackagesDir, entry.Name(), "INFO")
		raw, err := os.ReadFile(infoPath)
		if err != nil {
			continue
		}
		pkg := parsePackageInfo(entry.Name(), raw)
		if input.Prefix != "" && !strings.HasPrefix(strings.ToLower(pkg.Name), strings.ToLower(input.Prefix)) {
			continue
		}
		packages = append(packages, pkg)
	}

	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	return packageListResult{Packages: packages}, nil
}

func (s *server) packageInfo(args json.RawMessage) (map[string]any, error) {
	var input struct {
		Package string `json:"package"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	if input.Package == "" {
		return nil, errors.New("package is required")
	}
	pkg, err := s.readPackageInfo(input.Package)
	if err != nil {
		return nil, err
	}
	return map[string]any{"package": pkg}, nil
}

func (s *server) searchJournal(ctx context.Context, args json.RawMessage) (journalSearchResult, error) {
	var input struct {
		Unit  string `json:"unit"`
		Grep  string `json:"grep"`
		Since string `json:"since"`
		Lines int    `json:"lines"`
	}
	_ = json.Unmarshal(args, &input)
	if input.Lines <= 0 {
		input.Lines = s.cfg.MaxJournalLen
	}

	cmdArgs := []string{"--no-pager", "-o", "json", "-n", strconv.Itoa(input.Lines)}
	if input.Unit != "" {
		cmdArgs = append(cmdArgs, "-u", input.Unit)
	}
	if input.Since != "" {
		cmdArgs = append(cmdArgs, "--since", input.Since)
	}

	res := runCommand(ctx, s.cfg.JournalctlBin, cmdArgs...)
	if res.ExitCode != 0 {
		return journalSearchResult{}, fmt.Errorf("journalctl failed: %s", strings.TrimSpace(res.Stderr))
	}

	var entries []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(res.Stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			entries = append(entries, map[string]any{"raw": line})
			continue
		}
		entries = append(entries, obj)
	}
	if err := scanner.Err(); err != nil {
		return journalSearchResult{}, err
	}

	if input.Grep != "" {
		entries = filterJournalEntries(entries, input.Grep)
	}

	return journalSearchResult{Entries: entries, Count: len(entries)}, nil
}

func filterJournalEntries(entries []map[string]any, needle string) []map[string]any {
	if needle == "" {
		return entries
	}

	filtered := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if journalEntryMatches(entry, needle) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func journalEntryMatches(value any, needle string) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(v, needle)
	case map[string]any:
		for _, nested := range v {
			if journalEntryMatches(nested, needle) {
				return true
			}
		}
	case []any:
		for _, nested := range v {
			if journalEntryMatches(nested, needle) {
				return true
			}
		}
	}
	return false
}

func (s *server) removePackage(ctx context.Context, args json.RawMessage) (packageCommandResult, error) {
	var input struct {
		Package string `json:"package"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return packageCommandResult{}, err
	}
	if input.Package == "" {
		return packageCommandResult{}, errors.New("package is required")
	}
	if !s.cfg.AllowUninstall {
		return packageCommandResult{}, errors.New("uninstall is disabled by configuration")
	}
	pkg, err := s.readPackageInfo(input.Package)
	if err != nil {
		return packageCommandResult{}, err
	}
	if !pkg.CanUninstall {
		return packageCommandResult{}, fmt.Errorf("package %q is marked non-uninstallable in INFO", pkg.Name)
	}

	cmdRes := runCommand(ctx, s.cfg.SynopkgBin, "uninstall", pkg.Name)
	return packageCommandResult{
		Action:  "remove_package",
		Package: pkg.Name,
		Command: cmdRes,
	}, nil
}

func (s *server) installSPK(ctx context.Context, args json.RawMessage) (packageCommandResult, error) {
	var input struct {
		SPKPath   string `json:"spk_path"`
		SPKBase64 string `json:"spk_base64"`
		SPKURL    string `json:"spk_url"`
		SPKSHA256 string `json:"spk_sha256"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return packageCommandResult{}, err
	}

	var path string
	var cleanup func()
	var source string

	switch {
	case input.SPKPath != "":
		path = input.SPKPath
		source = "path"
	case input.SPKBase64 != "":
		data, err := base64.StdEncoding.DecodeString(input.SPKBase64)
		if err != nil {
			return packageCommandResult{}, err
		}
		f, err := os.CreateTemp("", "mcp-spk-*.spk")
		if err != nil {
			return packageCommandResult{}, err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			os.Remove(f.Name())
			return packageCommandResult{}, err
		}
		if err := f.Close(); err != nil {
			os.Remove(f.Name())
			return packageCommandResult{}, err
		}
		path = f.Name()
		source = "base64"
		cleanup = func() { _ = os.Remove(path) }
	case input.SPKURL != "":
		downloaded, err := downloadSPK(ctx, input.SPKURL, s.cfg.DownloadLimit)
		if err != nil {
			return packageCommandResult{}, err
		}
		path = downloaded
		source = "url"
		cleanup = func() { _ = os.Remove(path) }
	default:
		return packageCommandResult{}, errors.New("one of spk_path, spk_base64, or spk_url is required")
	}
	if cleanup != nil {
		defer cleanup()
	}

	if !strings.HasSuffix(strings.ToLower(path), ".spk") {
		return packageCommandResult{}, fmt.Errorf("refusing to install non-SPK file %q", path)
	}
	checksum, err := fileSHA256(path)
	if err != nil {
		return packageCommandResult{}, err
	}
	expected := normalizeSHA256(input.SPKSHA256)
	if expected != "" && !strings.EqualFold(expected, checksum) {
		return packageCommandResult{}, fmt.Errorf("checksum mismatch for %q: got %s want %s", path, checksum, expected)
	}
	cmdRes := runCommand(ctx, s.cfg.SynopkgBin, "install", path)
	return packageCommandResult{
		Action:           "install_spk",
		Source:           source,
		TempFile:         pathIfTemp(source, path),
		ChecksumSHA256:   checksum,
		ChecksumVerified: expected != "",
		Command:          cmdRes,
	}, nil
}

func normalizeSHA256(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	value = strings.TrimPrefix(value, "sha256:")
	return value
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot read SPK %q; upload the bytes to /spk-upload and install the returned temp file path: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *server) checkRuntime(ctx context.Context, args json.RawMessage) (runtimeHealthResult, error) {
	var input struct {
		Host      string   `json:"host"`
		TimeoutMS int      `json:"timeout_ms"`
		Services  []string `json:"services"`
		Ports     []int    `json:"ports"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return runtimeHealthResult{}, err
	}
	if len(input.Services) == 0 && len(input.Ports) == 0 {
		return runtimeHealthResult{}, errors.New("at least one service or port is required")
	}

	host := input.Host
	if host == "" {
		host = "127.0.0.1"
	}

	timeout := time.Second
	if input.TimeoutMS > 0 {
		timeout = time.Duration(input.TimeoutMS) * time.Millisecond
	}

	result := runtimeHealthResult{Host: host}
	healthy := true

	for _, service := range input.Services {
		cmdRes := runCommand(ctx, s.cfg.SynosystemctlBin, "get-active-status", service)
		serviceRes := serviceHealthResult{
			Name:    service,
			Command: cmdRes,
		}
		if cmdRes.ExitCode == 0 {
			status := strings.TrimSpace(strings.ToLower(cmdRes.Stdout))
			serviceRes.ActiveStatus = status
			serviceRes.Healthy = status == "active"
		}
		if !serviceRes.Healthy {
			healthy = false
		}
		result.Services = append(result.Services, serviceRes)
	}

	for _, port := range input.Ports {
		address := net.JoinHostPort(host, strconv.Itoa(port))
		portRes := portHealthResult{
			Host:    host,
			Port:    port,
			Address: address,
		}
		dialer := net.Dialer{Timeout: timeout}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			portRes.Error = err.Error()
			healthy = false
		} else {
			portRes.Healthy = true
			_ = conn.Close()
		}
		result.Ports = append(result.Ports, portRes)
	}

	result.Healthy = healthy
	return result, nil
}

func (s *server) servicePID(ctx context.Context, args json.RawMessage) (servicePidResult, error) {
	var input struct {
		Service     string `json:"service"`
		PreviousPID int    `json:"previous_pid"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return servicePidResult{}, err
	}
	if input.Service == "" {
		return servicePidResult{}, errors.New("service is required")
	}

	result := servicePidResult{Service: input.Service}
	if input.PreviousPID > 0 {
		result.PreviousPid = input.PreviousPID
	}

	statusRes := runCommand(ctx, s.cfg.SynosystemctlBin, "get-active-status", input.Service)
	result.Command = statusRes
	if statusRes.ExitCode != 0 {
		return result, nil
	}

	result.ActiveStatus = strings.TrimSpace(strings.ToLower(statusRes.Stdout))
	if result.ActiveStatus != "active" {
		result.Healthy = false
		return result, nil
	}

	pid, _, _ := s.lookupServicePID(ctx, input.Service)
	if pid > 0 {
		result.Pid = pid
	}

	if input.PreviousPID > 0 {
		result.PreviousPidAlive = processAlive(input.PreviousPID)
		result.Healthy = result.Pid > 0 && result.Pid != input.PreviousPID && !result.PreviousPidAlive
		return result, nil
	}

	result.Healthy = result.Pid > 0
	return result, nil
}

func (s *server) lookupServicePID(ctx context.Context, service string) (int, commandResult, error) {
	attempts := [][]string{
		{"get-pid", service},
		{"status", service},
		{"show", "-p", "MainPID", service},
		{"show", "-p", "MainPID", "--value", service},
	}

	var last commandResult
	for _, args := range attempts {
		cmdRes := runCommand(ctx, s.cfg.SynosystemctlBin, args...)
		last = cmdRes
		if cmdRes.ExitCode != 0 {
			continue
		}
		if pid, ok := parsePIDFromOutput(strings.Join(args, " "), cmdRes.Stdout); ok {
			return pid, cmdRes, nil
		}
	}
	return 0, last, errors.New("unable to determine service pid")
}

func parsePIDFromOutput(command string, stdout string) (int, bool) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return 0, false
	}

	if strings.Contains(command, "status") {
		scanner := bufio.NewScanner(strings.NewReader(trimmed))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			lower := strings.ToLower(line)
			if strings.Contains(lower, "pid") {
				if pid, ok := firstPositiveInt(line); ok {
					return pid, true
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return 0, false
		}
		return 0, false
	}

	return firstPositiveInt(trimmed)
}

func firstPositiveInt(raw string) (int, bool) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r < '0' || r > '9'
	})
	for _, field := range fields {
		if field == "" {
			continue
		}
		value, err := strconv.Atoi(field)
		if err == nil && value > 0 {
			return value, true
		}
	}
	return 0, false
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func pathIfTemp(source, path string) string {
	if source == "path" {
		return ""
	}
	return path
}

func writeUploadedSPK(r io.Reader) (string, string, int64, error) {
	const maxUploadedSPKSize = 256 << 20

	f, err := os.CreateTemp("", "mcp-spk-*.spk")
	if err != nil {
		return "", "", 0, err
	}

	h := sha256.New()
	limited := io.LimitReader(r, maxUploadedSPKSize+1)
	size, err := io.Copy(io.MultiWriter(f, h), limited)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", "", 0, err
	}
	if size > maxUploadedSPKSize {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", "", 0, fmt.Errorf("uploaded SPK exceeds %d bytes", maxUploadedSPKSize)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", "", 0, err
	}

	return f.Name(), hex.EncodeToString(h.Sum(nil)), size, nil
}

func downloadSPK(ctx context.Context, rawURL string, timeout time.Duration) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}

	f, err := os.CreateTemp("", "mcp-spk-*.spk")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func runCommand(ctx context.Context, bin string, args ...string) commandResult {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return commandResult{
		Command:  bin,
		Args:     append([]string(nil), args...),
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
}

func parsePackageInfo(dirName string, raw []byte) packageInfo {
	info := parseInfoFile(raw)
	name := firstNonEmpty(info["package"], info["package_name"], dirName)
	display := firstNonEmpty(info["displayname"], info["displayname_enu"], name)
	version := firstNonEmpty(info["package_version"], info["version"])
	desc := firstNonEmpty(info["description"], info["description_enu"])
	canUninstall := true
	if strings.EqualFold(info["ctl_uninstall"], "no") {
		canUninstall = false
	}
	return packageInfo{
		Name:         name,
		DisplayName:  display,
		Version:      version,
		Description:  desc,
		InstallPath:  info["install_dir"],
		CanUninstall: canUninstall,
		Info:         info,
	}
}

func (s *server) readPackageInfo(name string) (packageInfo, error) {
	infoPath := filepath.Join(s.cfg.PackagesDir, name, "INFO")
	raw, err := os.ReadFile(infoPath)
	if err != nil {
		return packageInfo{}, err
	}
	pkg := parsePackageInfo(name, raw)
	if pkg.Name == "" {
		pkg.Name = name
	}
	return pkg, nil
}

func parseInfoFile(raw []byte) map[string]string {
	out := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			if unq, err := strconv.Unquote(value); err == nil {
				value = unq
			} else {
				value = strings.Trim(value, "\"")
			}
		} else if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
			value = strings.Trim(value, "'")
		}
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func envBoolOrDefault(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
