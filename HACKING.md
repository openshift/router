# OpenShift Router Hacking

This is a living document containing suggestions for testing Router changes on an existing cluster.
These "hacks" are not officially supported and are subject to change, break, etc.

## Building

To build the Router binary, run:

```
$ make
```

## Developing

### Prerequisites

* An [OpenShift cluster](https://github.com/openshift/installer)
* An admin-scoped `KUBECONFIG` for the cluster.
* Install [imagebuilder](https://github.com/openshift/imagebuilder)

#### Building a Modified Router Image Locally & Deploying to the Cluster

To test Router changes on an available cluster, utilize `Dockerfile.debug` and
`Makefile.debug` in `hack/`.

`Dockerfile.debug` is a multi-stage dockerifle for building the Router binary,
as well as the Router image itself. The outputted image uses `centos:8` as it's base
since installing packages on an OpenShift RHEL base image requires RHEL entitlements.

`Makefile.debug` contains simple commands for "hot-swapping" the Router image running
in an IngressController deployment.

Example:

1. Run `make` to ensure that your code changes compile
1. Set the `IMAGE` environment variable. (ie. `export IMAGE=<your-quay-username>/openshift-router`)
1. Build a modified Router image for testing your changes (`make -f hack/Makefile.debug new-openshift-router-image`)
1. Push the new Router image to quay.io (`make -f hack/Makefile.debug push`)
1. Use the new Router image in the default Ingress Controller's deployment (`make -f hack/Makefile.debug set-image`)

Alternatively, after setting `IMAGE`, you can run `make dwim` (do what I mean) to accomplish the above steps in one command.

In case OpenShift is deployed as single node, `push` can be changed to `scp` by running `make dwim-single` instead. Node needs sshkey configured. No need to set `IMAGE` envvar.

When done testing, use `make -f hack/Makefile.debug reset` to re-enable the CVO and Ingress Operator.

## Tests

Run unit tests:
```
$ make check
```

## Local Run and Debugging

The steps below configures the local development environment to run OpenShift router.

> Note that a local router cannot properly proxy requests, since it cannot reach the overlay network in the
Kubernetes cluster. This mode is mostly useful for debugging controller behavior and configuration builder,
improving the "code + run + test + debug" workflow on these scenarios. Once the router seems to be building
proper configurations, a fully working OpenShift environment should be used to complete the exploratory e2e
tests.

### Install HAProxy

HAProxy needs to be installed, either from the package manager or compiled from the sources. The steps below
describe how to compile HAProxy from sources.

```bash
sudo yum install -y pcre2-devel
mkdir -p ~/ws/haproxy
cd ~/ws/haproxy
# Choose a version from https://www.haproxy.org, column "Latest version" points to the source tarball.
curl -LO https://www.haproxy.org/download/2.8/src/haproxy-2.8.18.tar.gz
tar xzf haproxy-2.8.18.tar.gz
cd haproxy-2.8.18/
make TARGET=linux-glibc USE_OPENSSL=1 USE_PROMEX=1 USE_PCRE2=1 USE_PCRE2_JIT=1
./haproxy -vv
sudo cp haproxy /usr/sbin/
```

### Run OpenShift router locally

Prepare the local environment and start router with default options:

```bash
make local-run
```

Prepare means create and clean `/var/lib/haproxy` and sub-directories, which router uses to configure HAProxy.

### Debug locally

Here is the VSCode launch configuration that starts OpenShift router in debug mode.

> Run `make local-prepare` before the very first run, and whenever a clean environment is needed.

`.vscode/launch.json` content, merge its content with any previous one:

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Launch OpenShift router",
            "type": "go",
            "request": "launch",
            "mode": "debug",
            "cwd": ".",
            "program": "cmd/openshift-router",
            "env": {
                "KUBECONFIG": "/PATH/TO/KUBECONFIG.yaml",
                "ROUTER_SERVICE_HTTP_PORT": "9090",
                "ROUTER_SERVICE_HTTPS_PORT": "9443",
                "STATS_USERNAME": "admin",
                "STATS_PASSWORD": "admin",
                "STATS_PORT": "1936",
            },
            "args": [
                "--template=images/router/haproxy/conf/haproxy-config.template",
                "--reload=images/router/haproxy/reload-haproxy",
            ],
        }
    ]
}
```

Caveats:

1. On debug mode, the binary name is not `openshift-router`, making the bootstrap code to fail. Temporarily patch `cmd/openshift-router/main.go` and hardcode `openshift-router` in the `CommandFor()` call.
1. A haproxy instance should be left behind depending on how the router is stopped. Run `killall haproxy` in case the router complains when trying to listen to sockets.
