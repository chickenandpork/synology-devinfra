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
	"syscall"
	"testing"
	"time"
)

func TestDiagnosticConfig(t *testing.T) {
	t.Setenv("MCP_SYNO_PACKAGE_LOGS_DIR", "")
	t.Setenv("MCP_SYNO_DOCKER_COMPOSE_BIN", "")
	cfg := defaultConfig()
	if cfg.PackageLogsDir != "/var/log/packages" || cfg.DockerComposeBin != "/var/packages/ContainerManager/target/usr/bin/docker-compose" {
		t.Fatalf("incorrect diagnostic defaults: %+v", cfg)
	}
	t.Setenv("MCP_SYNO_PACKAGE_LOGS_DIR", "/test/log/packages")
	t.Setenv("MCP_SYNO_DOCKER_COMPOSE_BIN", "/test/bin/docker-compose")
	cfg = defaultConfig()
	if cfg.PackageLogsDir != "/test/log/packages" || cfg.DockerComposeBin != "/test/bin/docker-compose" {
		t.Fatalf("ignored diagnostic overrides: %+v", cfg)
	}
}

func TestInspectPackagePathRejectsFIFO(t *testing.T) {
	cfg := defaultConfig()
	cfg.PackagesDir = t.TempDir()
	root := filepath.Join(cfg.PackagesDir, "example", "var")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := newServer(cfg).inspectPackagePath(json.RawMessage(`{"package":"example","area":"var","path":"pipe"}`))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("expected rejection of FIFO, got %v", err)
		}
	case <-time.After(time.Second):
		// Unblock a regressed reader so the test does not leave a hanging goroutine.
		f, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		<-done
		t.Fatal("inspection blocked opening a FIFO")
	}
}

func TestRuntimeHTTPProbes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.Write([]byte(`{"database":"ok"}`))
		case "/redirect":
			http.Redirect(w, r, "/failed", http.StatusFound)
		case "/large":
			w.Write([]byte(strings.Repeat("x", 8192)))
		default:
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	srv := newServer(defaultConfig())
	for _, tc := range []struct {
		path    string
		status  int
		healthy bool
	}{
		{"/api/health", 200, true}, {"/redirect", 302, true}, {"/failed", 503, false}, {"/large", 200, true},
	} {
		args := json.RawMessage(fmt.Sprintf(`{"http":[{"port":%d,"path":%q}]}`, port, tc.path))
		res, err := srv.checkRuntime(context.Background(), args)
		if err != nil || len(res.HTTP) != 1 || res.HTTP[0].Status != tc.status || res.Healthy != tc.healthy {
			t.Fatalf("%s: %+v, %v", tc.path, res, err)
		}
		if len(res.HTTP[0].Body) > 4096 {
			t.Fatal("unbounded HTTP body")
		}
		if tc.path == "/api/health" && res.HTTP[0].Body != `{"database":"ok"}` {
			t.Fatal("missing health body")
		}
	}
	for _, args := range []string{`{"http":[{"port":0,"path":"/"}]}`, `{"http":[{"port":65536,"path":"/"}]}`, `{"http":[{"port":3000,"path":"relative"}]}`} {
		if _, err := srv.checkRuntime(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
	server.Close()
	res, err := srv.checkRuntime(context.Background(), json.RawMessage(fmt.Sprintf(`{"http":[{"port":%d,"path":"/"}]}`, port)))
	if err != nil || res.Healthy || res.HTTP[0].Error == "" {
		t.Fatalf("unreachable: %+v, %v", res, err)
	}
}

func TestInspectPackagePath(t *testing.T) {
	cfg := defaultConfig()
	cfg.PackagesDir = t.TempDir()
	root := filepath.Join(cfg.PackagesDir, "example", "var")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "worker.log"), []byte(strings.Repeat("a", 65536)+"last"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	srv := newServer(cfg)
	res, err := srv.inspectPackagePath(json.RawMessage(`{"package":"example","area":"var","path":"worker.log"}`))
	if err != nil || res["truncated"] != true || len(res["text"].(string)) != 65536 || !strings.HasSuffix(res["text"].(string), "last") {
		t.Fatalf("read = %v, %v", res, err)
	}
	res, err = srv.inspectPackagePath(json.RawMessage(`{"package":"example","area":"var"}`))
	if err != nil || strings.Join(res["entries"].([]string), ",") != "escape,worker.log" {
		t.Fatalf("list = %v, %v", res, err)
	}
	for _, args := range []string{
		`{"package":"../example","area":"var"}`, `{"package":"example","area":".."}`,
		`{"package":"example","area":"var","path":"../conf"}`,
		`{"package":"example","area":"var","path":"/etc/passwd"}`,
		`{"package":"example","area":"var","path":"escape"}`, `{`,
	} {
		if _, err := srv.inspectPackagePath(json.RawMessage(args)); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
}

func TestInspectPackagePathSymlinkedArea(t *testing.T) {
	cfg := defaultConfig()
	cfg.PackagesDir = t.TempDir()
	pkg := filepath.Join(cfg.PackagesDir, "example")
	if err := os.Mkdir(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	area := t.TempDir()
	if err := os.Symlink(area, filepath.Join(pkg, "var")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(area, "worker.log"), []byte("worker output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("worker.log", filepath.Join(area, "current.log")); err != nil {
		t.Fatal(err)
	}
	srv := newServer(cfg)
	res, err := srv.inspectPackagePath(json.RawMessage(`{"package":"example","area":"var","path":"current.log"}`))
	if err != nil || res["text"] != "worker output\n" {
		t.Fatalf("read within symlinked area = %v, %v", res, err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("outside the package area"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(area, "outside.log")); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.inspectPackagePath(json.RawMessage(`{"package":"example","area":"var","path":"outside.log"}`)); err == nil {
		t.Fatal("followed symlink outside the package area")
	}
}

func TestReadPackageLog(t *testing.T) {
	cfg := defaultConfig()
	cfg.PackageLogsDir = filepath.Join(t.TempDir(), "packages")
	if err := os.Mkdir(cfg.PackageLogsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.PackageLogsDir, "example-package.log"), []byte("old\nworker failed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newServer(cfg)
	res, err := srv.readPackageLog(context.Background(), json.RawMessage(`{"package":"example-package","lines":1}`))
	if err != nil || res.ExitCode != 0 || res.Stdout != "worker failed\n" {
		t.Fatalf("readPackageLog = %+v, %v", res, err)
	}
	if err := os.WriteFile(filepath.Join(cfg.PackageLogsDir, "..", "synopkg.log"), []byte("old\ncompose worker failed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err = srv.readPackageLog(context.Background(), json.RawMessage(`{"package":"example-package","source":"installer","lines":1}`))
	if err != nil || res.ExitCode != 0 || res.Stdout != "compose worker failed\n" {
		t.Fatalf("read installer log = %+v, %v", res, err)
	}
	if err := os.WriteFile(filepath.Join(cfg.PackageLogsDir, "..", "messages"), []byte("system worker error\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err = srv.readPackageLog(context.Background(), json.RawMessage(`{"package":"example-package","source":"messages","lines":1}`))
	if err != nil || res.ExitCode != 0 || res.Stdout != "system worker error\n" {
		t.Fatalf("read system messages = %+v, %v", res, err)
	}
	for _, args := range []string{
		`{"package":"../messages"}`, `{"package":"/etc/passwd"}`,
		`{"package":""}`, `{"package":"example-package","lines":-1}`,
		`{"package":"example-package","lines":2001}`, `{`,
		`{"package":"example-package","source":"/etc/passwd"}`,
		`{"package":"../messages","source":"installer"}`,
	} {
		if _, err := srv.readPackageLog(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("accepted invalid arguments: %s", args)
		}
	}
}

func TestDockerInspect(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := defaultConfig()
	cfg.DockerComposeBin = filepath.Join(dir, "docker")
	srv := newServer(cfg)
	res, err := srv.dockerInspect(context.Background(), json.RawMessage(`{"kind":"volume","name":"example-volume"}`))
	if err != nil || res.ExitCode != 0 || res.Stdout != "volume\ninspect\n--format\n{{json .}}\n--\nexample-volume\n" {
		t.Fatalf("dockerInspect = %+v, %v", res, err)
	}
	for _, args := range []string{`{"kind":"volume","name":"--help"}`, `{"kind":"rm","name":"example-volume"}`, `{"kind":"image"}`} {
		if _, err := srv.dockerInspect(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("accepted invalid arguments: %s", args)
		}
	}
	res, err = srv.dockerCompose(context.Background(), json.RawMessage(`{"package":"example-package","project":"example-project","action":"config"}`))
	if err != nil || res.ExitCode != 0 || res.Stdout != "--file\n/var/packages/example-package/target/example-project/compose.yaml\nconfig\n--quiet\n" {
		t.Fatalf("dockerCompose = %+v, %v", res, err)
	}
	for _, args := range []string{
		`{"package":"../escape","project":"example-project","action":"config"}`,
		`{"package":"example-package","project":"..","action":"logs"}`,
		`{"package":"example-package","project":"example-project","action":"down"}`,
		`{"package":"example-package","project":"example-project","action":"config","file":"../secret"}`,
	} {
		if _, err := srv.dockerCompose(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("accepted invalid arguments: %s", args)
		}
	}
	res, err = srv.dockerCompose(context.Background(), json.RawMessage(`{"package":"example-package","project":"example-project","action":"config","file":"docker-compose.admin.yaml"}`))
	if err != nil || res.ExitCode != 0 || res.Stdout != "--file\n/var/packages/example-package/target/example-project/docker-compose.admin.yaml\nconfig\n--quiet\n" {
		t.Fatalf("admin dockerCompose = %+v, %v", res, err)
	}
}

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
	foundRestart := false
	for _, tool := range list.Tools {
		if tool.Name == "restart_service" {
			foundRestart = true
			break
		}
	}
	if !foundRestart {
		t.Fatalf("expected restart_service in tools/list")
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

func TestRestartServiceToolRestartsActiveService(t *testing.T) {
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "synosystemctl")
	script := `#!/bin/sh
case "$1" in
  get-active-status)
    printf 'active\n'
    ;;
  restart)
    printf 'restarted %s\n' "$2"
    ;;
  start)
    echo "unexpected start" >&2
    exit 42
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cfg := defaultConfig()
	cfg.SynosystemctlBin = scriptPath
	srv := newServer(cfg)

	res, err := srv.restartService(context.Background(), mustJSON(t, map[string]any{
		"service": "pkg-user-victoriametrics-victoria-metrics.service",
	}))
	if err != nil {
		t.Fatalf("restartService: %v", err)
	}
	if res.Service != "pkg-user-victoriametrics-victoria-metrics.service" {
		t.Fatalf("service = %q", res.Service)
	}
	if res.Action != "restart" {
		t.Fatalf("action = %q, want restart", res.Action)
	}
	if res.ActiveStatus != "active" {
		t.Fatalf("activeStatus = %q, want active", res.ActiveStatus)
	}
	if res.StatusCommand.ExitCode != 0 {
		t.Fatalf("status command exit = %d", res.StatusCommand.ExitCode)
	}
	if res.Command.ExitCode != 0 {
		t.Fatalf("restart command exit = %d", res.Command.ExitCode)
	}
	if got, want := res.Command.Args, []string{"restart", "pkg-user-victoriametrics-victoria-metrics.service"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("restart args = %v, want %v", got, want)
	}
}

func TestRestartServiceToolStartsInactiveService(t *testing.T) {
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "synosystemctl")
	script := `#!/bin/sh
case "$1" in
  get-active-status)
    printf 'inactive\n'
    ;;
  start)
    printf 'started %s\n' "$2"
    ;;
  restart)
    echo "unexpected restart" >&2
    exit 42
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cfg := defaultConfig()
	cfg.SynosystemctlBin = scriptPath
	srv := newServer(cfg)

	res, err := srv.restartService(context.Background(), mustJSON(t, map[string]any{
		"service": "pkg-user-victoriametrics-victoria-logs.service",
	}))
	if err != nil {
		t.Fatalf("restartService: %v", err)
	}
	if res.Service != "pkg-user-victoriametrics-victoria-logs.service" {
		t.Fatalf("service = %q", res.Service)
	}
	if res.Action != "start" {
		t.Fatalf("action = %q, want start", res.Action)
	}
	if res.ActiveStatus != "inactive" {
		t.Fatalf("activeStatus = %q, want inactive", res.ActiveStatus)
	}
	if res.StatusCommand.ExitCode != 0 {
		t.Fatalf("status command exit = %d", res.StatusCommand.ExitCode)
	}
	if res.Command.ExitCode != 0 {
		t.Fatalf("start command exit = %d", res.Command.ExitCode)
	}
	if got, want := res.Command.Args, []string{"start", "pkg-user-victoriametrics-victoria-logs.service"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("start args = %v, want %v", got, want)
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
