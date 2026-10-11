# Grafana OSS Compose SPK

Build with `bazel build //spk/grafana-oci:grafana-oci`. The installable package is
`bazel-bin/spk/grafana-oci/grafana-oci.spk`.

This interim package follows `spk/bazel-remote-cache` and uses the shared
`dockercompose` helper and DSM's Docker project worker. Install Synology
Container Manager before installing this SPK. The SPK contains the Compose
definition; Container Manager downloads the official `grafana/grafana` OSS
image at deployment time. Its version and multi-architecture digest are pinned
in `.github/renovate-regex.json` for Renovate updates.

Grafana runs as the upstream non-root user `grafana` (UID 472), exposes port 3000, and is
available at `http://<server>:3000/` after installation. The SPK requests no elevated
permissions. Credentials are not embedded in this package. Complete Grafana's
initial login and password setup, then use the account provisioning command below.

Use the named user in Compose: DSM's Docker project worker removes quotes from
numeric user values, producing a configuration that Compose rejects.

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

## Account provisioning

`//spk/grafana-oci:provision` is a reusable command that calls Grafana's HTTP API
directly. It is separate from this SPK and from the Synology MCP. Keep the account
configuration and credentials in the deployment repository, encrypted with that
repository's secret-management system.

The tool accepts this generic credentials shape:

```json
{
  "username": "server-administrator-login",
  "password": "server-administrator-password",
  "users": {
    "owner": "owner-password",
    "maintenance": "maintenance-password",
    "viewer-a": "viewer-a-password",
    "viewer-b": "viewer-b-password"
  }
}
```

The `users` entries supply creation passwords, and existing passwords are never
reset. The Admin API requires a server administrator's basic authentication
credentials; a service account token cannot create global users. The account
configuration has this generic shape:

```json
{
  "org_id": 1,
  "users": [
    {"login": "owner", "role": "Admin", "server_admin": true},
    {"login": "maintenance", "role": "Admin", "server_admin": false},
    {"login": "viewer-a", "role": "Viewer", "server_admin": false},
    {"login": "viewer-b", "role": "Viewer", "server_admin": false}
  ]
}
```

```sh
bazel run //spk/grafana-oci:provision -- \
  --url http://grafana.example.test:3000 \
  --config /path/to/accounts.json \
  --secrets /path/to/credentials.age \
  --age-identity /path/to/age-identity
```

The default previews account roles without changing Grafana. Append `--apply`
to create missing users, reconcile roles and server administrator flags, and
verify the resulting permissions. Existing users not in the configuration are
left alone, including the bootstrap administrator. Organization roles are managed
only in the configured organization. Changes are sequential, not transactional;
after a failure, correct the cause and rerun the command.

### Recovery after recreating Grafana

Recreating the container while retaining the named `grafana-oci` volume preserves
the database, including accounts and passwords. If that volume is lost or a new
empty database is started, run:

```sh
bazel run //spk/grafana-oci:provision -- \
  --url http://grafana.example.test:3000 \
  --config /path/to/accounts.json \
  --secrets /path/to/credentials.age \
  --age-identity /path/to/age-identity \
  --bootstrap --apply
```

Install the `age` command locally. Decryption stays in the provisioning process's
memory. The command first tries the saved owner's credentials. If those fail with
HTTP 401 and `--bootstrap` is present, it permits bootstrap only when the database
contains a single initial administrator accepting `admin/admin`. It renames that
the configured owner account, changes its password to the saved password, and
creates the other accounts using their saved passwords and roles. It can resume
if interrupted between renaming the initial account and changing its password.
Repeating the command on the configured instance preserves existing passwords.

Omit `--apply` to preview recovery without changes. A populated database whose
administrator password differs from the saved one requires database restore or
administrator recovery; this command does not reset unknown existing passwords.
Dashboard definitions and data sources must also be restored separately. If the
account configuration contains home dashboard UIDs, restore those dashboards first.
Keep `credentials.age`, the account configuration, and the age identity backed up;
the identity must stay separate from the repository.

After deploying dashboards from code with stable UIDs, add
`"home_dashboard_uid": "your-dashboard-uid"` to each display account. The command
uses that account's credential from `users` to set its personal home dashboard,
checks that the account can read the dashboard, and preserves other preferences.
An empty UID restores Grafana's default home; omitting the field leaves it alone.
Viewer role prevents dashboard edits but a home dashboard preference does not
restrict which other dashboards the user can read. Use folder permissions for
that distinction. Dashboard definitions and publication are separate from this
account command.
