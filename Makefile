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

all: build

build:
	$(GO_BUILD_RECIPE)

images/router/*/Dockerfile: images/router/base/Dockerfile
	imagebuilder -t registry.svc.ci.openshift.org/openshift/origin-v4.0:`basename $(@D)`-router -f images/router/`basename $(@D)`/Dockerfile .

images/router/*/Dockerfile.ocp: images/router/base/Dockerfile.ocp
	imagebuilder -t registry.svc.ci.openshift.org/ocp/4.0:`basename $(@D)`-router -f images/router/`basename $(@D)`/Dockerfile.ocp .

check:
	CGO_ENABLED=1 $(GO) test -race ./...

.PHONY: verify
verify:
	hack/verify-gofmt.sh
	hack/verify-deps.sh

# OTE test extension binary configuration
TESTS_EXT_DIR := ./tests-extension
TESTS_EXT_BINARY := tests-extension/bin/router-tests-ext

# Build OTE extension binary
.PHONY: tests-ext-build
tests-ext-build:
	@echo "Building OTE test extension binary..."
	@cd $(TESTS_EXT_DIR) && $(MAKE) build
	@echo "OTE binary built successfully at $(TESTS_EXT_BINARY)"

# Compress OTE extension binary (for CI/CD and container builds)
.PHONY: tests-ext-compress
tests-ext-compress: tests-ext-build
	@echo "Compressing OTE extension binary..."
	@gzip -f $(TESTS_EXT_BINARY)
	@echo "Compressed binary created at $(TESTS_EXT_BINARY).gz"

# Copy compressed binary to _output directory (for CI/CD)
.PHONY: tests-ext-copy
tests-ext-copy: tests-ext-compress
	@echo "Copying compressed binary to _output..."
	@mkdir -p _output
	@cp $(TESTS_EXT_BINARY).gz _output/
	@echo "Binary copied to _output/router-tests-ext.gz"

# Alias for backward compatibility
.PHONY: extension
extension: tests-ext-build

# Clean extension binary
.PHONY: clean-extension
clean-extension:
	@echo "Cleaning extension binary..."
	@rm -f $(TESTS_EXT_BINARY) $(TESTS_EXT_BINARY).gz _output/router-tests-ext.gz
	@cd $(TESTS_EXT_DIR) && $(MAKE) clean
