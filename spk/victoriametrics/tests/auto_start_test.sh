#!/bin/sh
set -eu

postinst=$1
spk=$2
shift 2

assert_line() {
    file=$1
    expected=$2
    if ! grep -Fqx "$expected" "$file"; then
        echo "$file is missing: $expected" >&2
        exit 1
    fi
}

for unit in "$@"; do
    assert_line "$unit" "[Install]"
    assert_line "$unit" "WantedBy=multi-user.target"
done

# The pkg-user-* transition made these services fail before ExecStart on Soko.
# Keep package-owned units as pkg-victoriametrics-*; DSM's package resource
# manager handles their lifecycle without the unworkable user-unit transition.
for unit in \
    pkg-victoriametrics-victoria-metrics.service \
    pkg-victoriametrics-victoria-logs.service \
    pkg-victoriametrics-vmagent.service \
    pkg-victoriametrics-avahi.service; do
    if ! tar -tf "$spk" | grep -Fqx "conf/systemd/$unit"; then
        echo "$spk is missing the package-owned unit: $unit" >&2
        exit 1
    fi
done

if tar -tf "$spk" | grep -Fq 'conf/systemd/pkg-user-victoriametrics-'; then
    echo "$spk must not contain pkg-user-victoriametrics-* units" >&2
    exit 1
fi

# Synology's package scripts cannot modify the systemd enable-status file. The
# systemd resource manager enables units from their [Install] metadata.
if grep -Fq "synosystemctl enable" "$postinst"; then
    echo "$postinst must not call synosystemctl enable" >&2
    exit 1
fi
