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

Do not use SSH to access `soko`/`soko.fern`, even for read-only queries. All
NAS inspection and changes must use the Synology MCP. If the MCP lacks a
required capability, extend, build, deploy, activate, and then use mcpserver
before proceeding.

Third-party SPKs must never request elevated permissions. Do not add
`run_as_root`, root control scripts, or equivalent privilege declarations to an
SPK; only Synology-owned SPKs may use elevated privileges.
