def _platform_suffix_transition_impl(settings, attr):
    return {
        platform.name: {
            "//command_line_option:platforms": [str(platform)],
        }
        for platform in attr.platforms
    }


platform_suffix_transition = transition(
    implementation = _platform_suffix_transition_impl,
    inputs = [],
    outputs = ["//command_line_option:platforms"],
)


def _platform_suffixed_spks_impl(ctx):
    outputs = []

    for suffix in sorted(ctx.split_attr.base_spk.keys()):
        dep = ctx.split_attr.base_spk[suffix]
        src = dep[DefaultInfo].files.to_list()[0]
        out = ctx.actions.declare_file("{}-{}.spk".format(ctx.attr.package_basename, suffix))

        ctx.actions.run_shell(
            inputs = [src],
            outputs = [out],
            command = "cp \"$1\" \"$2\"",
            arguments = [src.path, out.path],
            mnemonic = "SuffixSpk",
        )

        outputs.append(out)

    return [DefaultInfo(files = depset(outputs))]


platform_suffixed_spks = rule(
    implementation = _platform_suffixed_spks_impl,
    attrs = {
        "base_spk": attr.label(
            cfg = platform_suffix_transition,
            mandatory = True,
            providers = [DefaultInfo],
        ),
        "package_basename": attr.string(mandatory = True),
        "platforms": attr.label_list(mandatory = True),
    },
)
