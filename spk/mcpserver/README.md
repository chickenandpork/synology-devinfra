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
- `check_runtime`
- `service_pid`
- `remove_package`

## Configuration

- `MCP_SYNO_PACKAGES_DIR` defaults to `/var/packages`
- `MCP_SYNO_SYNOPKG_BIN` defaults to `synopkg`
- `MCP_SYNO_SYNOSYSTEMCTL_BIN` defaults to `synosystemctl`
- `MCP_SYNO_JOURNALCTL_BIN` defaults to `journalctl`
- `MCP_SYNO_JOURNAL_LINES` defaults to `200`
- `MCP_SYNO_DOWNLOAD_TIMEOUT` defaults to `2m`
- `MCP_SYNO_ALLOW_UNINSTALL` defaults to `true`
- `MCP_SYNO_HTTP_BIND` defaults to `127.0.0.1:8787` in `serve` mode
- `MCP_SYNO_HTTP_PATH` defaults to `/mcp`
- `MCP_SYNO_HTTP_TOKEN` enables bearer auth when set

`install_spk` accepts `spk_sha256` so the caller can verify the transferred payload before installation.
`service_pid` reports the current PID for a Synology service and can confirm that a previous PID has disappeared after an upgrade.
`search_journal` filters entries in `mcpserver` itself, so `grep` works even when Synology's `journalctl` build lacks `--grep`.

To send the SPK bytes directly to the server, `POST` or `PUT` the raw file body to:

```bash
curl --data-binary @mcpserver-denverton-7.1.spk http://soko.local:8787/spk-upload
```

The response includes a server-side temp file path and a `sha256` checksum. Pass that temp file path to `install_spk` to install the uploaded package.

## Codex

Codex can use the HTTP MCP server at the default URL:

`http://soko.local:8787/mcp`

Add it to `~/.codex/config.toml`:

```toml
[mcp_servers.synology]
url = "http://soko.local:8787/mcp"
```

Or register it from the Codex CLI:

```bash
codex mcp add synology --url http://soko.local:8787/mcp
```

## Metrics

VictoriaMetrics can scrape the Prometheus endpoint at:

`http://soko.local:8787/metrics`

It exports basic health and activity metrics such as `mcpserver_up`, `mcpserver_start_time_seconds`, `mcpserver_last_activity_seconds`, `mcpserver_http_requests_total`, `mcpserver_rpc_requests_total`, and `mcpserver_tool_calls_total`.
