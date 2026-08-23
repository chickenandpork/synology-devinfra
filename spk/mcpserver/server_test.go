package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	var result callToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result")
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
