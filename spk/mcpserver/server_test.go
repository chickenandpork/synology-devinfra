package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseInfoFile(t *testing.T) {
	raw := []byte(`
# comment
package="foo"
displayname="Foo Package"
package_version=1.2.3-1
ctl_uninstall=no
description="Test package"
`)
	info := parseInfoFile(raw)
	if got, want := info["package"], "foo"; got != want {
		t.Fatalf("package = %q, want %q", got, want)
	}
	if got, want := info["displayname"], "Foo Package"; got != want {
		t.Fatalf("displayname = %q, want %q", got, want)
	}
	if got, want := info["ctl_uninstall"], "no"; got != want {
		t.Fatalf("ctl_uninstall = %q, want %q", got, want)
	}
}

func TestParsePackageInfo(t *testing.T) {
	pkg := parsePackageInfo("foo", []byte(`
package="foo"
displayname="Foo Package"
package_version="1.2.3-1"
ctl_uninstall=no
description="Test package"
`))
	if pkg.Name != "foo" {
		t.Fatalf("Name = %q", pkg.Name)
	}
	if pkg.DisplayName != "Foo Package" {
		t.Fatalf("DisplayName = %q", pkg.DisplayName)
	}
	if pkg.CanUninstall {
		t.Fatalf("CanUninstall = true, want false")
	}
}

func TestHTTPHandlerToolsList(t *testing.T) {
	srv := newServer(defaultConfig())
	handler := srv.httpMux(httpConfig{Path: "/mcp"})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if got, want := rr.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	var resp rpcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var list listToolsResult
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}
	if len(list.Tools) == 0 {
		t.Fatalf("expected at least one tool in tools/list")
	}
}

func TestSPKUploadAndInstall(t *testing.T) {
	payload := []byte("mcpserver spk stream")
	sum := sha256.Sum256(payload)
	wantChecksum := fmt.Sprintf("%x", sum[:])

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "synopkg")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write synopkg script: %v", err)
	}

	cfg := defaultConfig()
	cfg.SynopkgBin = scriptPath
	srv := newServer(cfg)
	handler := srv.httpMux(httpConfig{Path: "/mcp"})

	uploadReq := httptest.NewRequest(http.MethodPost, "/spk-upload", bytes.NewReader(payload))
	uploadReq.Header.Set("Content-Type", "application/octet-stream")
	uploadRR := httptest.NewRecorder()
	handler.ServeHTTP(uploadRR, uploadReq)

	if got, want := uploadRR.Code, http.StatusOK; got != want {
		t.Fatalf("upload status = %d, want %d", got, want)
	}

	var uploadRes spkUploadResult
	if err := json.Unmarshal(uploadRR.Body.Bytes(), &uploadRes); err != nil {
		t.Fatalf("unmarshal upload response: %v", err)
	}
	if uploadRes.ChecksumSHA256 != wantChecksum {
		t.Fatalf("checksum = %q, want %q", uploadRes.ChecksumSHA256, wantChecksum)
	}
	if uploadRes.TempFile == "" {
		t.Fatalf("expected temp file path in upload response")
	}

	gotPayload, err := os.ReadFile(uploadRes.TempFile)
	if err != nil {
		t.Fatalf("read uploaded temp file: %v", err)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("uploaded payload mismatch: got %q want %q", gotPayload, payload)
	}

	installArgs, err := json.Marshal(map[string]any{
		"spk_path":   uploadRes.TempFile,
		"spk_sha256": uploadRes.ChecksumSHA256,
	})
	if err != nil {
		t.Fatalf("marshal install args: %v", err)
	}
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "install_spk",
			"arguments": json.RawMessage(installArgs),
		},
	})
	if err != nil {
		t.Fatalf("marshal install request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(reqBody))
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got, want := rr.Code, http.StatusOK; got != want {
		t.Fatalf("install status = %d, want %d", got, want)
	}

	var resp rpcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal install response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected install error: %+v", resp.Error)
	}

	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal install result: %v", err)
	}
	var result callToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal install tool result: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected install success result")
	}

	structuredRaw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var install packageCommandResult
	if err := json.Unmarshal(structuredRaw, &install); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if !install.ChecksumVerified {
		t.Fatalf("expected checksum verification to be reported")
	}
	if install.ChecksumSHA256 != wantChecksum {
		t.Fatalf("install checksum = %q, want %q", install.ChecksumSHA256, wantChecksum)
	}
	if install.Command.ExitCode != 0 {
		t.Fatalf("unexpected synopkg exit code: %+v", install.Command)
	}
}

func TestInstallSPKMissingLocalPathHint(t *testing.T) {
	srv := newServer(defaultConfig())
	missing := filepath.Join(t.TempDir(), "missing.spk")

	_, err := srv.installSPK(context.Background(), mustJSON(t, map[string]any{
		"spk_path": missing,
	}))
	if err == nil {
		t.Fatalf("expected installSPK to fail for missing local path")
	}
	if !strings.Contains(err.Error(), "/spk-upload") {
		t.Fatalf("error = %q, want /spk-upload hint", err)
	}
}

func TestCheckRuntimeTool(t *testing.T) {
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "synosystemctl")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf 'active\\n'\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := defaultConfig()
	cfg.SynosystemctlBin = scriptPath
	srv := newServer(cfg)
	handler := srv.httpMux(httpConfig{Path: "/mcp"})

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"check_runtime","arguments":{"host":"127.0.0.1","timeout_ms":250,"services":["pkg-user-victoriametrics-victoria-metrics.service"],"ports":[` + strconv.Itoa(port) + `]}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(reqBody))
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got, want := rr.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	var resp rpcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var result callToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result")
	}

	structuredRaw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var health runtimeHealthResult
	if err := json.Unmarshal(structuredRaw, &health); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if !health.Healthy {
		t.Fatalf("expected healthy result: %+v", health)
	}
	if len(health.Services) != 1 || !health.Services[0].Healthy || health.Services[0].ActiveStatus != "active" {
		t.Fatalf("unexpected service health: %+v", health.Services)
	}
	if len(health.Ports) != 1 || !health.Ports[0].Healthy {
		t.Fatalf("unexpected port health: %+v", health.Ports)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return raw
}

func TestSearchJournalFiltersEntriesLocally(t *testing.T) {
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "journalctl")
	script := `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "--grep" ]; then
    echo "unexpected --grep forwarding" >&2
    exit 42
  fi
done
cat <<'EOF'
{"MESSAGE":"keep this line","_SYSTEMD_UNIT":"pkg-user-mcpserver.service"}
{"MESSAGE":"drop this line","_SYSTEMD_UNIT":"pkg-user-mcpserver.service"}
EOF
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cfg := defaultConfig()
	cfg.JournalctlBin = scriptPath
	srv := newServer(cfg)

	res, err := srv.searchJournal(context.Background(), json.RawMessage(`{"grep":"keep"}`))
	if err != nil {
		t.Fatalf("searchJournal: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("count = %d, want 1", res.Count)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(res.Entries))
	}
	if got := res.Entries[0]["MESSAGE"]; got != "keep this line" {
		t.Fatalf("message = %v, want %q", got, "keep this line")
	}
}

func TestServicePIDTool(t *testing.T) {
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "synosystemctl")
	script := `#!/bin/sh
case "$1" in
  get-active-status)
    printf 'active\n'
    ;;
  get-pid)
    exit 1
    ;;
  status)
    printf 'Main PID: 4242\n'
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	previousPID := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait process: %v", err)
	}

	cfg := defaultConfig()
	cfg.SynosystemctlBin = scriptPath
	srv := newServer(cfg)
	handler := srv.httpMux(httpConfig{Path: "/mcp"})

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"service_pid","arguments":{"service":"pkg-user-victoriametrics-victoria-metrics.service","previous_pid":` + strconv.Itoa(previousPID) + `}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(reqBody))
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got, want := rr.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	var resp rpcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var result callToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result")
	}

	structuredRaw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var pid servicePidResult
	if err := json.Unmarshal(structuredRaw, &pid); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if !pid.Healthy {
		t.Fatalf("expected healthy result: %+v", pid)
	}
	if pid.Pid != 4242 {
		t.Fatalf("pid = %d, want 4242", pid.Pid)
	}
	if pid.PreviousPid != previousPID {
		t.Fatalf("previousPid = %d, want %d", pid.PreviousPid, previousPID)
	}
	if pid.PreviousPidAlive {
		t.Fatalf("expected previous pid to be gone: %+v", pid)
	}
	if pid.ActiveStatus != "active" {
		t.Fatalf("activeStatus = %q, want %q", pid.ActiveStatus, "active")
	}
}

func TestFileSHA256AndNormalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.spk")
	data := []byte("payload")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := fileSHA256(path)
	if err != nil {
		t.Fatalf("fileSHA256: %v", err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(data))
	if got != want {
		t.Fatalf("fileSHA256 = %q, want %q", got, want)
	}

	if got := normalizeSHA256("sha256:" + strings.ToUpper(want)); got != want {
		t.Fatalf("normalizeSHA256 = %q, want %q", got, want)
	}
}

func TestHTTPMuxMetrics(t *testing.T) {
	cfg := defaultConfig()
	cfg.PackagesDir = t.TempDir()

	srv := newServer(cfg)
	handler := srv.httpMux(httpConfig{Path: "/mcp"})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_packages","arguments":{}}}`))
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got, want := rr.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRR := httptest.NewRecorder()
	handler.ServeHTTP(metricsRR, metricsReq)

	if got, want := metricsRR.Code, http.StatusOK; got != want {
		t.Fatalf("metrics status = %d, want %d", got, want)
	}

	body := metricsRR.Body.String()
	for _, want := range []string{
		"mcpserver_up 1",
		`mcpserver_http_requests_total{route="/mcp",method="POST",status="200"} 1`,
		`mcpserver_rpc_requests_total{method="tools/call",status="ok"} 1`,
		`mcpserver_tool_calls_total{tool="list_packages",status="ok"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
}

func TestHTTPHandlerRejectsBadToken(t *testing.T) {
	srv := newServer(defaultConfig())
	handler := srv.httpHandler(httpConfig{Path: "/mcp", Token: "secret"})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if got, want := rr.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}
