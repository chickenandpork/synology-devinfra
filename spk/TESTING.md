# SPK Testing

Keep SPK verification rules in a separate `tests/` package beside each SPK.

For every SPK:

- Verify the outer SPK archive contents.
- Verify the nested `package.tgz` contents.
- Sanity-check every static config file shipped in the package.
- If a `docker-compose` file exists, validate it as both compose input and plain YAML.
- Always verify that `conf/privilege` is present.
- For compiled binaries, verify the target platform with `file` or `readelf`.

Prefer small, explicit archive tests over broad ad hoc checks in the main `BUILD.bazel`.
