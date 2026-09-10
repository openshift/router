#!/bin/bash

set -eo pipefail

die() { echo "$*" >&2; exit 1; }

help() {
    echo "Usage: $(basename "$0") <command>"
    echo
    echo "  Commands:"
    echo
    echo "    prepare:   Prepares the environment without starting the router."
    echo "               Note that it kills any running HAProxy and cleans its"
    echo "               configuration directory."
    echo "    run:       Prepares the environment and starts the router locally."
    echo "    help:      Shows this help message."
    exit 0
}

router_prepare() {
    # dropping any previously running haproxy instance
    killall haproxy 2>/dev/null || :

    # copying mandatory files to HAProxy's configuration dir
    readonly haproxydir="/var/lib/haproxy"
    if ! [ -d "$haproxydir" ] || ! [ -w "$haproxydir" ]; then
        echo "We're going to ask your sudo password in order to create and fix permission on $haproxydir"
        sudo mkdir -p "$haproxydir"
        sudo chown "${USER}:${USER}" "$haproxydir"
    fi
    rm -rf "${haproxydir}/conf" "${haproxydir}/run"
    mkdir -p "${haproxydir}/conf" "${haproxydir}/run" "${haproxydir}/router/certs" "${haproxydir}/router/cacerts" "${haproxydir}/router/allowlists"
    cp images/router/haproxy/conf/* "${haproxydir}/conf/"
    touch "${haproxydir}/conf/haproxy.config"

    echo
    echo "$haproxydir prepared, starting HAProxy in foreground -- ^C to stop"

    # starting the "sidecar" haproxy
    haproxy -W -db -S "${haproxydir}/run/admin.sock,mode,600" -f "${haproxydir}/conf/haproxy.config"
}

router_run() {
    # changing listening ports, we usually don't have permission to bind to the default ones 80/443
    ROUTER_SERVICE_HTTP_PORT=9090 ROUTER_SERVICE_HTTPS_PORT=9443 STATS_USERNAME=admin STATS_PASSWORD=admin STATS_PORT=1936 ROUTER_GRACEFUL_SHUTDOWN_DELAY=2s \
        go run -v ./cmd/openshift-router \
            --template images/router/haproxy/conf/haproxy-config.template \
            --haproxy-admin-unix-socket /var/lib/haproxy/run/admin.sock
}

[ $# -ne 1 ] && help

# goes to repository root, we use relative paths
cd "$(dirname $0)/.."

case "$1" in
    prepare) router_prepare;;
    run) router_run;;
    help) help;;
    *) die "invalid command: $1";;
esac
