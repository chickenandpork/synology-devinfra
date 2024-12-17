def cleanup_redis_vanity_version(version):
    # ie 6.2.6-v17
    return version.replace("-v",".")
