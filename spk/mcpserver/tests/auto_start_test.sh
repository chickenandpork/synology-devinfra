#!/bin/sh
set -eu

unit=$1
postinst=$2

assert_line() {
    file=$1
    expected=$2
    if ! grep -Fqx "$expected" "$file"; then
        echo "$file is missing: $expected" >&2
        exit 1
    fi
}

# The [Install] section makes the unit eligible for boot activation.
assert_line "$unit" "[Install]"
assert_line "$unit" "WantedBy=multi-user.target"

# Synology's package scripts do not have permission to modify the systemd
# enable-status file. The systemd resource manager handles enablement from the
# unit metadata above; calling enable from postinst only produces an installer
# error and must not be reintroduced.
if grep -Fq "synosystemctl enable pkg-user-mcpserver.service" "$postinst"; then
    echo "$postinst must not call synosystemctl enable" >&2
    exit 1
fi

