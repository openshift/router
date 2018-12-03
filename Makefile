.PHONY: all build check images/router/*/Dockerfile images/router/*/Dockerfile.rhel

all: build

build:
	go build -ldflags '-X github.com/openshift/router/vendor/k8s.io/client-go/pkg/version.gitVersion=$(shell git describe) -X github.com/openshift/router/vendor/k8s.io/client-go/pkg/version.gitCommit=$(shell git rev-parse HEAD)' ./cmd/openshift-router

images/router/*/Dockerfile: images/router/base/Dockerfile
	imagebuilder -t registry.svc.ci.openshift.org/openshift/origin-v4.0:`basename $(@D)`-router -f images/router/`basename $(@D)`/Dockerfile .

images/router/*/Dockerfile.rhel: images/router/base/Dockerfile.rhel
	imagebuilder -t registry.svc.ci.openshift.org/ocp/4.0:`basename $(@D)`-router -f images/router/`basename $(@D)`/Dockerfile.rhel .

check:
	go test -race ./...
