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

This permission covers queries, not unrestricted changes to the host.
