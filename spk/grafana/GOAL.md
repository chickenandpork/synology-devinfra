# Native Grafana OSS Synology SPK

Status: deferred in favor of the interim `//spk/grafana-oci:grafana-oci`
Docker Compose distribution. This document records the native implementation
objective; the OCI package does not satisfy the native acceptance criteria.

## Objective and dependency blocker

`bazel build //spk/grafana:grafana` must produce an installable native Grafana OSS
SPK for Soko (Synology DS1819+, Denverton/x86-64). Use an official **precompiled**
Linux AMD64 release; the user confirmed this choice over source compilation.
Do not compile Grafana, invoke Go/Node/Yarn, use a container, introduce spksrc,
or implement a separate SPK archive builder for this native target.

Use the repository's `rules_synology` dependency and its actual public APIs,
preferring its highest-level primitives. Inspection of the pinned version 0.2.3
(also the newest published release when checked on 2026-09-30) found:

- `info_file`, `privilege_config`, `data_share`, `resource_config`, and
  `systemd_user_unit` provide metadata and DSM resource declarations.
- There is no complete native SPK construction rule. Upstream examples and this
  repository assemble the payload and outer SPK using `rules_pkg`'s `pkg_tar`.
- `systemd_user_unit` registers a DSM resource; the package still supplies the
  unit and lifecycle scripts. `info_file` accepts a combined `package_version`,
  rather than separate upstream-version and revision attributes.

The original specification explicitly forbids `pkg_tar`, shell tar assembly,
and an independent SPK implementation. **Resolve this blocker before creating
the native target:** add/use a reusable upstream rules_synology SPK primitive,
or obtain an explicit adjustment allowing its documented archive assembly
pattern. No adjustment has been approved yet.

API reference: https://github.com/chickenandpork/rules_synology/blob/v0.2.3/defs.bzl

## Release, layout, and licensing

- Pin the official OSS release version, URL, and verified SHA-256 in the normal
  central third-party dependency location. Never use a moving download URL.
- Treat the extracted release as runtime payload, preserving `bin/`, `public/`,
  `conf/`, bundled plugins (including `plugins-bundled/` wherever the release
  places it), other required assets, and all license/notice material.
- Preserve upstream branding and generated frontend assets unchanged.
- Set `GRAFANA_HOME` and `GF_PATHS_HOME` to `/var/packages/grafana/target`.
  This entire directory is immutable, replaceable package content.
- Keep upstream and package revision separately maintained, rendering the SPK
  version as, for example, `13.2.3-1`; packaging fixes increment only the revision.
- Use Denverton/x86-64 package metadata and repository platform conventions,
  never `noarch`. Allow future artifact selection for other architectures.
- Packaging needs no host Grafana, Go, Node, Yarn, or Docker installation and
  should be reproducible offline once Bazel dependencies are cached.

## Persistent DSM shared storage

Create/reuse four genuine DSM shares through rules_synology/DSM mechanisms:

| Share | Contents / runtime variable |
| --- | --- |
| GrafanaData | `GF_PATHS_DATA`, including `grafana.db` |
| GrafanaConfig | `grafana.ini` (`GF_PATHS_CONFIG`) and `provisioning/` (`GF_PATHS_PROVISIONING`) |
| GrafanaLogs | `GF_PATHS_LOGS` |
| GrafanaPlugins | `GF_PATHS_PLUGINS`, for user-installed plugins |

Each share must be independently placeable on an administrator-selected DSM
volume. Never hard-code `/volumeN`, store mutable data under the package target,
or create ordinary directories disguised as DSM shares. DSM's
`/var/packages/grafana/shares/<name>` symlinks may resolve the actual shared
storage; the contents themselves must live outside `/var/packages/grafana`.

Use `data_share` and the DSM data-share worker. Verify independent volume
placement, including reuse of shares created on selected volumes in DSM.
Grant only the dedicated package service account the necessary read/write
access, without broad grants to unrelated users. Do not delete shares on
upgrade. Follow DSM's persistent-storage uninstall policy: the documented
data-share worker preserves shared folders on package removal.

Create a minimal valid `grafana.ini` only if absent. Create missing provisioning
subdirectories (datasources, dashboards, alerting, etc.) without replacing
existing configuration. Do not embed passwords or other credentials.

## Service behavior

- Run `/var/packages/grafana/target/bin/grafana server` in the foreground, with
  an appropriate working directory and all six `GF_PATHS_*` paths explicit.
- Use DSM `start-stop-status` and the systemd service integration supplied by
  rules_synology. DSM/systemd owns supervision and graceful termination.
- Start, Stop, Restart, and Status must operate on and report the actual
  service. Do not use custom supervisors, PID polling, nohup, cron, or similar.
- Run under a dedicated unprivileged DSM package account. All package scripts
  must also comply with AGENTS.md: third-party SPKs must never request root or
  elevated permissions, including root control scripts.
- Listen on HTTP port 3000. Do not bundle a reverse proxy or VictoriaMetrics.
  Do not invent a VictoriaMetrics endpoint. Optional datasource provisioning
  is appropriate only if an established repository mechanism exists.

## Upgrade contract

1. Stop Grafana cleanly through DSM/systemd.
2. Replace the complete immutable package payload.
3. Preserve all four shares, configuration, database, and installed plugins.
4. Start the new Grafana against the preserved state, allowing Grafana's own
   database migrations to run.

Do not preserve obsolete `bin/`, `public/`, bundled plugins, or upstream
`conf/defaults.ini` from the previous SPK.

## Validation and manual acceptance

Build and test with Bazel. Inspect the SPK for checksum-pinned input,
`bin/grafana`, frontend and runtime assets, license material, Denverton
architecture, unprivileged configuration, DSM share declarations, runtime
paths, and absence of source-build machinery. Run `grafana --version` in a
compatible Linux environment where practical.

Then install on Soko through the Synology MCP (never SSH), extending the MCP
first if inspection or lifecycle capabilities are missing. Verify:

1. DSM recognizes the package; Start, Stop, Restart, and Status work.
2. The service runs as the package user and the installed `public/` exists.
3. All four shares exist, with independently selectable storage volumes.
4. `http://soko:3000/` serves the complete Grafana UI.
5. Create persistent state, including a dashboard or datasource.
6. Upgrade to a newer/rebuilt SPK and prove the payload was replaced while
   database, configuration, logs, and plugins survive and Grafana restarts.

Completion requires both the single native build target and successful
installation/upgrade acceptance, not just a structurally valid archive.
