def convert_ubuntu_vanity_version_to_semver(version):
    for k, v in {"jammy-": "22.04."}.items():
        version = version.replace(k, v)
    return version
