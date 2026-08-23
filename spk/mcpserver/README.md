# mcpserver

Synology MCP server for package inspection and local package operations.

It runs in two modes:

- stdio MCP, for local process launch
- `serve`, a network MCP endpoint at `http://<host>:8787/mcp`

## Tools

- `list_packages`
- `package_info`
- `search_journal`
- `install_spk`
- `remove_package`

## Configuration

- `MCP_SYNO_PACKAGES_DIR` defaults to `/var/packages`
- `MCP_SYNO_SYNOPKG_BIN` defaults to `synopkg`
- `MCP_SYNO_JOURNALCTL_BIN` defaults to `journalctl`
- `MCP_SYNO_JOURNAL_LINES` defaults to `200`
- `MCP_SYNO_DOWNLOAD_TIMEOUT` defaults to `2m`
- `MCP_SYNO_ALLOW_UNINSTALL` defaults to `true`
- `MCP_SYNO_HTTP_BIND` defaults to `127.0.0.1:8787` in `serve` mode
- `MCP_SYNO_HTTP_PATH` defaults to `/mcp`
- `MCP_SYNO_HTTP_TOKEN` enables bearer auth when set

## Codex

Codex can use the HTTP MCP server at the default URL:

`http://synology.local:8787/mcp`

Add it to `~/.codex/config.toml`:

```toml
[mcp_servers.synology]
url = "http://synology.local:8787/mcp"
```

Or register it from the Codex CLI:

```bash
codex mcp add synology --url http://synology.local:8787/mcp
```

## Metrics

VictoriaMetrics can scrape the Prometheus endpoint at:

`http://synology.local:8787/metrics`

It exports basic health and activity metrics such as `mcpserver_up`, `mcpserver_start_time_seconds`, `mcpserver_last_activity_seconds`, `mcpserver_http_requests_total`, `mcpserver_rpc_requests_total`, and `mcpserver_tool_calls_total`.
