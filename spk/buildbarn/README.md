# Buildbarn SPK

This package deploys:

- `frontend`: client-facing `bb_storage`
- `storage`: the backend `bb_storage` replica
- `scheduler`: `bb_scheduler`

Example client endpoint:

```sh
bazel build --remote_cache=grpc://<synology-host>:8980 --remote_instance_name=foo //...
```

The scheduler admin UI is exposed on `http://<synology-host>:7982`.
