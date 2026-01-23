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
