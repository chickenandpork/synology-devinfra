# Grafana OSS Compose SPK

Build with `bazel build //spk/grafana-oci:grafana-oci`. The installable package is
`bazel-bin/spk/grafana-oci/grafana-oci.spk`.

This interim package follows `spk/bazel-remote-cache` and uses the shared
`dockercompose` helper and DSM's Docker project worker. Install Synology
Container Manager before installing this SPK. The SPK contains the Compose
definition; Container Manager downloads the official `grafana/grafana` OSS
image at deployment time. Its version and multi-architecture digest are pinned
in `.github/renovate-regex.json` for Renovate updates.

Grafana runs as the upstream non-root user (UID 472), exposes port 3000, and is
available at `http://soko:3000/` after installation. The SPK requests no elevated
permissions. Credentials are not configured in this package; complete
Grafana's initial login and password setup in the UI.

The named Docker volume `grafana-oci` stores the database, dashboards,
datasources, and installed plugins beneath `/var/lib/grafana`. Compose creates
it during installation and reuses the stable name on updates. Back it up before
upgrades; explicitly deleting the volume in Container Manager deletes its data.
Its storage location follows Container Manager's Docker volume storage.
Configuration uses the image defaults plus the Compose environment, and logs
go to the container log. No VictoriaMetrics datasource is provisioned.

This is separate from the deferred native package described in
`../grafana/GOAL.md`, including its four independently placeable DSM shares.
The two packages use separate identities but both default to port 3000, so stop
this deployment before starting a future native installation on that port.
Migration of the Docker volume into native DSM shares is not automatic.

After installing, check DSM Start/Stop/Status, the Grafana UI and `/api/health`,
then create a dashboard and upgrade the package to verify persistence.
