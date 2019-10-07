.PHONY: all build check images/router/*/Dockerfile images/router/*/Dockerfile.rhel

PACKAGE=github.com/openshift/router
MAIN_PACKAGE=$(PACKAGE)/cmd/openshift-router

BIN=$(lastword $(subst /, ,$(MAIN_PACKAGE)))

ifneq ($(DELVE),)
GO_GCFLAGS ?= -gcflags=all="-N -l"
endif

GO_LDFLAGS ?= -ldflags '-X github.com/openshift/router/vendor/k8s.io/client-go/pkg/version.gitVersion=$(shell git describe) -X github.com/openshift/router/vendor/k8s.io/client-go/pkg/version.gitCommit=$(shell git rev-parse HEAD)'

GO=GO111MODULE=on GOFLAGS=-mod=vendor go
GO_BUILD_RECIPE=CGO_ENABLED=0 $(GO) build -o $(BIN) $(GO_GCFLAGS) $(GO_LDFLAGS) $(MAIN_PACKAGE)


all: build

build:
	$(GO_BUILD_RECIPE)

images/router/*/Dockerfile: images/router/base/Dockerfile
	imagebuilder -t registry.svc.ci.openshift.org/openshift/origin-v4.0:`basename $(@D)`-router -f images/router/`basename $(@D)`/Dockerfile .

images/router/*/Dockerfile.rhel: images/router/base/Dockerfile.rhel
	imagebuilder -t registry.svc.ci.openshift.org/ocp/4.0:`basename $(@D)`-router -f images/router/`basename $(@D)`/Dockerfile.rhel .

check:
	$(GO) test -race ./...
