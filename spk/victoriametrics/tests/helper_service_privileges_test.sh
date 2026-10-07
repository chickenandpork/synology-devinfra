#!/bin/sh
set -eu

# Parse the packaged helper unit without starting any services.
awk '
function trim(s) {
    sub(/^[ \t]+/, "", s)
    sub(/[ \t\r]+$/, "", s)
    return s
}
function fail(message) {
    print FILENAME ": " message
    failed = 1
}
{
    line = trim($0)
    if (line ~ /^[#;]/ || line == "") next
    if (line ~ /^\[/) {
        service = (line == "[Service]")
        next
    }
    if (!service) next
    separator = index(line, "=")
    if (!separator) next
    key = trim(substr(line, 1, separator - 1))
    value = trim(substr(line, separator + 1))
    if (key == "User") user = value
    if (key == "Group" && value ~ /^(root|0)$/)
        fail("helper must not request the root group")
    if (key ~ /^Exec/ && value ~ /^[-:@|]*[+!]/)
        fail(key " must not bypass service credentials")
    if (key == "PermissionsStartOnly" && value ~ /^(yes|true|1|on)$/)
        fail("PermissionsStartOnly must not elevate helper commands")
    if (key == "AmbientCapabilities" && value != "")
        fail("helper must not request ambient capabilities")
}
END {
    if (user == "")
        fail("helper needs an explicit unprivileged User=; system services default to root")
    else if (user !~ /^[a-zA-Z_][a-zA-Z0-9_-]*[$]?$/ || user == "root")
        fail("helper User= must name an unprivileged account")
    exit failed
}
' "$1"
