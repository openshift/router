#!/bin/bash

set -eo pipefail

die() { echo "$*" >&2; exit 1; }

help() {
    echo "Usage: $(basename "$0") <command>"
    echo
    echo "  Commands:"
    echo
    echo "    prepare:   Prepares the environment and starts HAProxy, without"
    echo "               starting the router. Note that it kills any HAProxy"
    echo "               previously started by prepare, and cleans the router"
    echo "               and HAProxy configuration directories. HAProxy runs in"
    echo "               the foreground, blocking the terminal."
    echo "    run:       Starts the router locally, in the foreground, blocking the"
    echo "               terminal. Requires a running HAProxy started by prepare."
    echo "    help:      Shows this help message."
    exit 0
}

# haproxydir is the directory where the router writes the configuration files used by HAProxy.
readonly haproxydir="/var/lib/haproxy"

router_prepare() {
    # stop the HAProxy previously started by prepare, if any; it is identified
    # by its admin socket, so that unrelated HAProxy processes are not affected
    pkill -f "${haproxydir}/run/admin.sock" 2>/dev/null || :

    # copying mandatory files to HAProxy's configuration dir
    if ! [ -d "$haproxydir" ] || ! [ -w "$haproxydir" ]; then
        echo "We're going to ask your sudo password in order to create and fix permission on $haproxydir"
        sudo mkdir -p "$haproxydir"
        sudo chown "${USER}:${USER}" "$haproxydir"
    fi
    rm -rf "${haproxydir}/conf" "${haproxydir}/run" "${haproxydir}/router"
    mkdir -p "${haproxydir}/conf" "${haproxydir}/run" "${haproxydir}/router/certs" "${haproxydir}/router/cacerts" "${haproxydir}/router/allowlists"
    cp images/router/haproxy/conf/* "${haproxydir}/conf/"
    touch "${haproxydir}/conf/haproxy.config"

    echo
    echo "$haproxydir prepared, starting HAProxy in foreground -- ^C to stop"

    # starting the "sidecar" haproxy
    haproxy -W -db -S "${haproxydir}/run/admin.sock,mode,600" -f "${haproxydir}/conf/haproxy.config"
}

router_run() {
    # check that socat is installed first, so that a missing socat is not reported as an unresponsive HAProxy
    if ! command -v socat >/dev/null 2>&1; then
        die "socat is not installed, it is needed to check the HAProxy admin socket."
    fi
    # check not only that the socket exists, but also that a running HAProxy is listening on it
    if ! (echo show version | socat - "${haproxydir}/run/admin.sock" >/dev/null); then
        die "haproxy admin socket is not responding, run make local-prepare in another terminal first."
    fi

    # changing listening ports, we usually don't have permission to bind to the default ones 80/443
    ROUTER_SERVICE_HTTP_PORT=9090 ROUTER_SERVICE_HTTPS_PORT=9443 STATS_USERNAME=admin STATS_PASSWORD=admin STATS_PORT=1936 ROUTER_GRACEFUL_SHUTDOWN_DELAY=2s \
        go run -v ./cmd/openshift-router \
            --template images/router/haproxy/conf/haproxy-config.template \
            --haproxy-admin-unix-socket "${haproxydir}/run/admin.sock"
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
