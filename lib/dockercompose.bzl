load("@aspect_bazel_lib//lib:expand_template.bzl", "expand_template_rule")
load("@bazel_skylib//rules:copy_file.bzl", "copy_file")
load("@bazel_skylib//rules:write_file.bzl", "write_file")
load("@local_renovate_regex//:json.bzl", "version_json")
load("@rules_pkg//pkg:mappings.bzl", "pkg_attributes", "pkg_files")
load("@rules_pkg//pkg/private/tar:tar.bzl", "SUPPORTED_TAR_COMPRESSIONS", "pkg_tar")
load(
    "@rules_synology//:defs.bzl",
    "docker_compose",
    "docker_project",
    "images",
    "info_file",
    "privilege_config",
    "resource_config",
    _SPK_REQUIRED = "SPK_REQUIRED_SCRIPTS",
)

SPK_REQUIRED_SCRIPTS = ["start-stop-status"] + _SPK_REQUIRED

def dockercompose(
        name,
        project,
        compose,  # Label pointing to docker-compose file
        description,
        icon_file,  # ie "@buchgr_icon//file"
        maintainer,  # ie "//:chickenandpork"
        healthcheck_containernames = [],
        os_min_ver = "7.0-1",  # correct-format=[^\d+(\.\d+){1,2}(-\d+){1,2}$]
        package_version = "0.0.0-0",
        preexisting_volumes = [],
        username = None):
    docker_compose(
        name = "{}~docker_compose".format(name),
        compose = ":dockercompose",
        path = project,
        project_name = project,
    )

    docker_project(
        name = "{}~docker_project".format(name),
        projects = [":{}~docker_compose".format(name)],
    )

    # check via `bazel query //spk/minecraft-bedrock:info --output=build`
    info_file(
        name = "{}~info".format(name),
        package_name = project,
        #arch_strings = ["noarch"],
        description = description,
        maintainer = maintainer,
        os_min_ver = os_min_ver,
        package_version = package_version,
    )

    privilege_config(
        name = "{}~priv".format(name),
        username = username or "sc-{}".format(name),
        # run_as_root isn't working: Synology seems to throw a 313 or 319 error whenever I have any valid binaries in the run-as-root.  Need to optimize it over time.
        #run_as_root= [ "postinst", "preuninst"],
    )

    resource_config(
        name = "{}~rez".format(name),
        resources = ["{}~docker_project".format(name)],
    )

    # Logo taken from Jakob's page -- similar to how Jakob's photo is used on the docker image
    images(
        name = "{}~icons".format(name),
        src = icon_file,
    )

    pkg_files(
        name = "{}~conf".format(name),
        srcs = [
            ":{}~priv".format(name),
            ":{}~rez".format(name),
        ],
        attributes = pkg_attributes(
            mode = "0444",
        ),
        prefix = "conf",
        visibility = ["//visibility:public"],
    )

    pkg_tar(
        name = "{}~package".format(name),
        srcs = [],
        extension = "tgz",
        package_dir = "/",
        deps = ["{}~docker_project".format(name)],
    )

    #[ copy_file(name="{}-{}".format(name,k), out=k, is_executable=True, src=":{}~sss_script".format(name)) for k in SPK_REQUIRED_SCRIPTS ]

    pkg_files(
        name = "{}~scripts".format(name),
        srcs = [":{}-{}".format(name, k) for k in SPK_REQUIRED_SCRIPTS],
        attributes = pkg_attributes(
            mode = "0755",
        ),
        prefix = "scripts",
    )

    pkg_tar(
        name = name,
        srcs = [
            "{}~{}".format(name, k)
            for k in ["conf", "icons", "info", "package", "scripts"]
            # ":conf",
            # ":icons",
            # ":info",
            # ":package",
            # ":scripts",
        ],
        remap_paths = {"/{}~package.tgz".format(name): "/package.tgz"},
        extension = "tar",
        package_file_name = "{}.spk".format(name),
        visibility = ["//visibility:public"],
    )

    [write_file(
        name = "{}-{}".format(name, k),
        out = k,
        content = [
            "#!/bin/sh",
            "",
            """case "$1" in""",
            """    start)""",
        ] + ["""        docker volume ls|grep -e "\\W{0}\\$" 2>/dev/null || docker volume create \"{0}\"""".format(v) for v in preexisting_volumes] + [
            """        ;;""",
            """    stop)""",
            """        ;;""",
            """    status)""",
        ] + ["""        /usr/local/bin/docker_inspect \"{0}\" | grep -q "\\"Status\\": \\"running\\"" || exit 1""".format(c) for c in healthcheck_containernames] + [
            """        ;;""",
            """    log)""",
        ] + ["""        docker logs \"{0}\"""".format(c) for c in healthcheck_containernames] + [
            """        ;;""",
            """esac""",
            "",
            "exit 0",
            "",  # force a newline after the script to simplify diff checks
        ],
        is_executable = True,
        #visibility = ["//visibility:public"],
    ) for k in SPK_REQUIRED_SCRIPTS]
