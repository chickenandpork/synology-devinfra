Create a Synology DSM 7 SPK package that provides the central observability backend for a small k3s cluster, optimized for very low operational and memory overhead.

The SPK is to run directly on Synology DSM, not in Docker and not in Kubernetes.

Package purpose

The package must install and manage these standalone Victoria components:

1. VictoriaMetrics single-node
    * Persistent metrics storage.
    * Default HTTP port 8428.
    * Receives Prometheus remote-write data from vmagent.
    * Provides PromQL/MetricsQL query endpoints.
2. VictoriaLogs single-node
    * Persistent log storage.
    * Default HTTP port 9428.
    * Receives logs from vlagent instances running as DaemonSets on the k3s nodes.
    * vlagent will write to:
        http://<synology>:9428/insert/native
3. vmagent
    * Runs centrally on the Synology.
    * Performs Prometheus-compatible scraping of the k3s environment.
    * Writes collected metrics to the local VictoriaMetrics instance.
    * It should be capable of scraping:
        * node_exporter on each k3s node.
        * kubelet /metrics.
        * kubelet /metrics/cadvisor.
        * vlagent /metrics on each node.
        * VictoriaMetrics, VictoriaLogs and vmagent’s own metrics where appropriate.
    * Configuration must be stored in a normal editable configuration file rather than compiled into scripts.

Do not install Prometheus, Loki, Fluent Bit, OpenTelemetry Collector, Elasticsearch, or the VictoriaMetrics Kubernetes operator.

Architecture

The intended data flow is:

k3s nodes
 ├─ node_exporter ─────────────┐
 ├─ kubelet /metrics ──────────┤
 ├─ kubelet /metrics/cadvisor ─┼──> Synology vmagent
 └─ vlagent /metrics ──────────┘          │
                                          v
                                  VictoriaMetrics
k3s node /var/log/pods/*
          │
       vlagent
          │
          └────────────────────────> VictoriaLogs

vlagent itself is not part of the Synology package. It will subsequently be deployed to k3s separately.

Synology packaging requirements

Produce a conventional DSM 7 .spk, following the Synology Package Developer Guide rather than inventing a custom installer.

The source tree should contain the appropriate DSM package structure, including at minimum:

INFO
package.tgz
scripts/
conf/

with proper DSM lifecycle handling for installation, startup, shutdown, upgrade and uninstall.

Use Synology’s package framework and start-stop-status semantics where applicable. The generated package must be installable through Package Center → Manual Install. Synology documents INFO, package.tgz, lifecycle scripts and conf as the standard SPK structure. (Synology Knowledge Center)

Provide a reproducible build target that emits the finished .spk. Prefer the official Synology Package Toolkit / pkgscripts-ng approach unless there is a concrete technical reason this package can be assembled more simply without it. Synology documents PkgCreate.py as the standard build/pack workflow. (Synology Knowledge Center)

Binary handling

Use official VictoriaMetrics release binaries.

Do not compile VictoriaMetrics merely for the sake of compiling it if upstream provides an appropriate binary for the target Synology architecture.

The build must:

* pin explicit VictoriaMetrics/VictoriaLogs versions;
* verify downloaded artifacts using upstream checksums when available;
* make component versions easy to update;
* avoid fetching mutable latest releases;
* support the Synology architecture targeted by this repository.

If the repository does not already establish the NAS CPU architecture or DSM version, detect what is already known from project files before making assumptions. Clearly document any remaining target-platform assumption.

Runtime layout

Keep executables, configuration and persistent data separate.

Use DSM package-managed locations for binaries/configuration and a persistent package data location for databases. Do not scatter files under arbitrary /usr/local paths.

Conceptually:

package/
  bin/
    victoria-metrics-prod
    victoria-logs-prod
    vmagent-prod
  etc/
    vmagent.yml
persistent-data/
  victoriametrics/
  victorialogs/
  vmagent/

The exact DSM paths must follow current Synology package conventions.

The package must preserve metric/log databases and user configuration across ordinary package upgrades.

Service management

All three processes must be independently started and supervised as part of the package:

victoria-metrics-prod
victoria-logs-prod
vmagent-prod

Implement:

* start
* stop
* restart
* status
* clean shutdown
* PID/process validation
* useful DSM logging
* failure reporting

Do not use fragile process matching such as an unqualified killall.

Services should run as a dedicated, unprivileged DSM package account wherever possible.

Storage

Make retention explicitly configurable.

Start with conservative defaults suitable for a home/small k3s cluster; do not assume unlimited NAS storage.

Provide documented configuration for at least:

* VictoriaMetrics retention period;
* VictoriaLogs retention period;
* VictoriaMetrics data path;
* VictoriaLogs data path;
* vmagent temporary/queue data path.

Do not configure VictoriaMetrics cluster mode. This is deliberately the single-node VictoriaMetrics deployment; upstream provides it as one standalone server. (VictoriaMetrics Docs)

vmagent configuration

Supply a useful initial vmagent.yml, but do not hard-code cluster node addresses into package scripts.

Make scrape targets easy to edit.

Start with jobs conceptually equivalent to:

scrape_configs:
  - job_name: node
    static_configs:
      - targets: []
  - job_name: kubelet
    static_configs:
      - targets: []
  - job_name: vlagent
    static_configs:
      - targets: []
  - job_name: victoria
    static_configs:
      - targets:
          - 127.0.0.1:8428
          - 127.0.0.1:9428

Adapt the final configuration correctly for authentication/TLS requirements on the kubelet endpoints.

vmagent should remote-write locally to VictoriaMetrics rather than requiring Prometheus. VictoriaMetrics explicitly supports vmagent as its Prometheus-compatible scraper/remote writer and recommends it where lower resource consumption is desirable. (VictoriaMetrics Docs)

Logs ingestion

VictoriaLogs must accept the native vlagent protocol.

The future k3s agents will run approximately as:

vlagent-prod
  -kubernetesCollector
  -remoteWrite.url=http://<synology>:9428/insert/native

This is an upstream-supported topology: vlagent can run per Kubernetes node, discover pod/container logs, add Kubernetes metadata, buffer locally during outages, and send them to a single VictoriaLogs server. (VictoriaMetrics Docs)

Do not implement log tailing on the Synology for Kubernetes logs; collection belongs on the k3s nodes.

Networking

At minimum expose:

8428/tcp  VictoriaMetrics HTTP/query/write
9428/tcp  VictoriaLogs HTTP/query/write

Determine whether vmagent’s HTTP/status endpoint should be LAN-accessible or bound to loopback.

Integrate with DSM firewall/package resource declarations as appropriate rather than modifying host firewall state with arbitrary shell commands.

Do not expose these services to the public Internet by default.

Self-monitoring

Ensure vmagent collects enough metrics to determine the health of the observability system itself.

Victoria components expose Prometheus-format /metrics endpoints; vlagent, for example, exposes collector, remote-write, error and buffering metrics on its HTTP metrics endpoint. (VictoriaMetrics Docs)

At minimum make it possible to observe:

* process availability;
* ingestion rate;
* scrape failures;
* remote-write failures;
* disk usage;
* VictoriaMetrics storage health;
* VictoriaLogs ingestion/storage health;
* vlagent queue/buffering or dropped-log conditions.

Do not add Grafana to this SPK in the first implementation. Keep the package narrowly focused on collection and storage. Grafana can be added separately if required.

Deliverables

The task is complete when the repository contains:

1. Complete SPK source/package structure.
2. Reproducible build command.
3. Pinned upstream versions/checksum validation.
4. Working DSM lifecycle scripts.
5. Runtime configuration templates.
6. Persistent storage handling.
7. Default conservative retention settings.
8. README covering:
    * building the SPK;
    * installing it;
    * starting/stopping it;
    * locations of configuration/data/logs;
    * ports;
    * changing retention;
    * adding k3s scrape targets;
    * configuring vlagent to send logs;
    * upgrading the package;
    * uninstall/data-retention behaviour.
9. A verification procedure demonstrating:
    * all three Synology processes are healthy;
    * a test Prometheus metric reaches VictoriaMetrics;
    * a test log reaches VictoriaLogs;
    * vmagent successfully scrapes at least one target;
    * services survive DSM package restart.

Prefer simple shell/package-framework implementation over introducing another runtime or service manager.

Do not merely write design documentation: implement the SPK, build it, and exercise whatever automated/static tests can be run in the development environment. Where actual DSM installation cannot be tested locally, provide exact manual validation commands and distinguish those untested DSM-specific steps from verified build/test results.

