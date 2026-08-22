# VictoriaMetrics Observability SPK

This package installs three standalone binaries on Synology DSM 7:

- VictoriaMetrics single-node on `8428/tcp`
- VictoriaLogs single-node on `9428/tcp`
- vmagent bound to `127.0.0.1:8429`

## Build

```bash
bazel build //spk/victoriametrics:spk
```

The SPK is written to `bazel-bin/spk/victoriametrics/victoriametrics.spk`.

## Install

Use Package Center -> Manual Install and select the built `.spk`.

## Service control

The package uses the DSM package scripts:

- `start`
- `stop`
- `restart`
- `status`

## Paths

- Config: `/var/packages/victoriametrics/shares/victoriametrics/etc`
- Data: `/var/packages/victoriametrics/shares/victoriametrics/data`
- Logs: `/var/packages/victoriametrics/var/log`

## Default retention

- VictoriaMetrics: `14d`
- VictoriaLogs: `7d`

Edit the corresponding `*.conf` files in the package share to change retention or storage paths.

## vmagent config

Edit `vmagent.yml` for scrape targets. The default file includes placeholders for:

- node exporter
- kubelet `/metrics`
- kubelet `/metrics/cadvisor`
- vlagent
- the local Victoria services

Kubelet targets should use HTTPS with a bearer token and TLS settings when you add real cluster endpoints.
