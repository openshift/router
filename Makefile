.PHONY: all build check images/router/*/Dockerfile images/router/*/Dockerfile.ocp

PACKAGE=github.com/openshift/router
MAIN_PACKAGE=$(PACKAGE)/cmd/openshift-router

BIN=$(lastword $(subst /, ,$(MAIN_PACKAGE)))

ifneq ($(DELVE),)
GO_GCFLAGS ?= -gcflags=all="-N -l"
endif

SOURCE_GIT_TAG ?=$(shell git describe --long --tags --abbrev=7 --match 'v[0-9]*' 2>/dev/null || echo 'v0.0.0-unknown')
SOURCE_GIT_COMMIT ?=$(shell git rev-parse --short "HEAD^{commit}" 2>/dev/null)
SOURCE_GIT_TREE_STATE ?=$(shell ( ( [ ! -d ".git/" ] || git diff --quiet ) && echo 'clean' ) || echo 'dirty')

define version-ldflags
-X $(1).versionFromGit="$(SOURCE_GIT_TAG)" \
-X $(1).commitFromGit="$(SOURCE_GIT_COMMIT)" \
-X $(1).gitTreeState="$(SOURCE_GIT_TREE_STATE)" \
-X $(1).buildDate="$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')"
endef
GO_LD_EXTRAFLAGS ?=
GO_LDFLAGS ?=-ldflags "-s -w $(call version-ldflags,$(PACKAGE)/pkg/version) $(GO_LD_EXTRAFLAGS)"

GO=GO111MODULE=on GOFLAGS=-mod=vendor go
GO_BUILD_RECIPE=CGO_ENABLED=1 $(GO) build -o $(BIN) $(GO_GCFLAGS) $(GO_LDFLAGS) $(MAIN_PACKAGE)

# Get all the packages with benchmark tests defined to prevent
# "go test" from entering all existing packages when "./.." is used.
BENCH_PKGS ?= $(shell \grep -lR '^func Benchmark' | xargs -I {} dirname {} | sed 's|^|./|' | paste -sd ' ')
BENCH_TEST ?= .
BENCH_TIME ?= 2s
BENCH_COUNT ?= 5

all: build

build:
	$(GO_BUILD_RECIPE)

images/router/*/Dockerfile: images/router/base/Dockerfile
	imagebuilder -t registry.svc.ci.openshift.org/openshift/origin-v4.0:`basename $(@D)`-router -f images/router/`basename $(@D)`/Dockerfile .

images/router/*/Dockerfile.ocp: images/router/base/Dockerfile.ocp
	imagebuilder -t registry.svc.ci.openshift.org/ocp/4.0:`basename $(@D)`-router -f images/router/`basename $(@D)`/Dockerfile.ocp .

check:
	CGO_ENABLED=1 $(GO) test -race ./...

.PHONY: bench
bench:
	@# -run flag is added to avoid running unit tests
	CGO_ENABLED=1 $(GO) test -bench=$(BENCH_TEST) -run='^#' -benchtime=$(BENCH_TIME) -count=$(BENCH_COUNT) -benchmem $(BENCH_PKGS)

.PHONY: verify
verify:
	hack/verify-gofmt.sh
	hack/verify-deps.sh
