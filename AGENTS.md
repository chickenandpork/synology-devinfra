# Working agreements

- Ask clarifying questions when scope is unclear.
- Keep changes small and reviewable.
- Run tests before finishing.

## Standing network permission

The user granted permanent permission on 2026-09-29 to query `soko.fern` using the Synology MCP
Server on tcp/8787 outside the sandbox during development in `synology-devinfra` and `dockside`.
Do not ask for conversational confirmation again for those queries. Use the required tool
escalation mechanism if the sandbox still requires it; this note does not itself change enforced
sandbox permissions.

Permission to use the configured MCP server includes DNS queries for its connection
hostname (including `soko.fern` and `soko.local`) outside the sandbox/container
when needed to establish the connection. Do not ask for separate conversational
confirmation; use tool escalation if enforced sandbox permissions require it.

## MCP server upgrades

Permission to add an API method to mcpserver implicitly includes building,
uploading through its SPK upload endpoint, installing, activating, and checking
the updated mcpserver after all builds and tests pass. This is the normal,
authorized upgrade workflow; do not ask for separate conversational confirmation.
Before deployment, tests must verify that `package.tgz` in the platform-specific
SPK contains a binary cross-compiled for the NAS architecture, not the build host.

Do not use SSH to access `soko`/`soko.fern`, even for read-only queries. All
NAS inspection and changes must use the Synology MCP. If the MCP lacks a
required capability, extend, build, deploy, activate, and then use mcpserver
before proceeding.

Third-party SPKs must never request elevated permissions. Do not add
`run_as_root`, root control scripts, or equivalent privilege declarations to an
SPK; only Synology-owned SPKs may use elevated privileges.
