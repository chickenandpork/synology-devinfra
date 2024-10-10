load("//json:load_json_file.bzl", "load_json_file")
#def _load_json_file_impl(repository_ctx):
#    """Generate a starlark equivalent of a parsed JSON file
#
#    Args:
#      repository_ctx: repository context
#    """
#
#    json_data = json.decode(repository_ctx.read(repository_ctx.path(Label("//:.github/renovate-regex.json"))))
#
#    repository_ctx.file("BUILD", "exports_files([ \"json.bzl\"])")
#    repository_ctx.file("json.bzl", "version_json=%s" % repr(json_data))
#
## DRY via JSON: Initial conversion of a JSON file to a dict/struct that Bazel can interpret.  This
## is a bit indirect: this is used during MODULE.bazel registration of external resources.  We
## create a virtual external dependency called @name that provides a file called "@name//:json.bzl"
## that provides a JSON struct called "version_json"; usage is similar to:
##
## load("@name//:json.bzl", "version_json")
## ...
##    version = version_json["aws-cli"]["version"]
## ...
#load_json_file = repository_rule(
#    implementation = _load_json_file_impl,
#    attrs = {},  # TBD -- if we need to parameterize this, we can expand this when the time arises
#    local = True,
#)

def _read_json_impl(_):
    load_json_file(name = "local_renovate_regex")

read_json = module_extension(implementation = _read_json_impl)
