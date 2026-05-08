# VictoriaMetrics SPK

[VictoriaMetrics](https://victoriametrics.com/) packaged as a Synology Package (SPK) for single-node deployment.

## About

VictoriaMetrics is a fast, cost-effective and scalable open source time series database. It is compatible with the Prometheus remote write API and query API.

### Single Binary Deployment

All functionality (ingestion, storage, querying, admin UI) runs in one process:

```bash
/var/packages/victoriametrics/target/bin/victoria-metrics \
    -retentionPeriod=1month \
    -storageDataPath=/var/packages/victoriametrics/shares/victoriametrics/data
```

### Ports

- **8428** - HTTP API (ingestion at `/api/v1/write`, query at `/api/v1/read`, admin UI at `/`)

### Prometheus Integration

VictoriaMetrics accepts Prometheus remote write:

```
POST http://<synology-ip>:8428/api/v1/write
```

Query via:

```
GET http://<synology-ip>:8428/api/v1/query?query=up
```

## Installation

Build the SPK:

```bash
bazel build spk/victoriametrics:spk
```

The resulting `.spk` file will be at `bazel-bin/spk/victoriametrics/victoriametrics.spk`.

Install via Synology Package Center or:

```bash
sudo synopkg install bazel-bin/spk/victoriametrics/victoriametrics.spk
```

## Architecture Support

- `denverton` (x86_64) - tested
- `armv8` (ARM64) - included in build
