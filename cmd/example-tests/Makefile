ROOT_DIR := $(realpath ../..)
BUILD_DIR := $(ROOT_DIR)/build
BINARY_NAME := example-tests

GO_MODULE := github.com/openshift/router
GO_PKG_NAME := $(GO_MODULE)/cmd/example-tests

GIT_COMMIT := $(shell git rev-parse --short HEAD)
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GIT_TREE_STATE := $(shell if git diff --quiet; then echo clean; else echo dirty; fi)

LDFLAGS := -X '$(GO_PKG_NAME)/pkg/version.CommitFromGit=$(GIT_COMMIT)' \
           -X '$(GO_PKG_NAME)/pkg/version.BuildDate=$(BUILD_DATE)' \
           -X '$(GO_PKG_NAME)/pkg/version.GitTreeState=$(GIT_TREE_STATE)'

#.PHONY: verify test lint clean unit integration example-tests framework-tests
.PHONY: verify test lint clean unit integration example-tests
all: unit build integration

verify: lint

#build: example-tests framework-tests
build: $(BUILD_DIR)/$(BINARY_NAME)
#build: 
#	cd $(ROOT_DIR) && go build -mod=vendor -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/example-tests

#example-tests:
#	# GO_COMPLIANCE_POLICY="exempt_all" must only be used for test related binaries.
#	# It prevents various FIPS compliance policies from being applied to this compilation.
#	# Do not set globally.
#	GO_COMPLIANCE_POLICY="exempt_all" go build -ldflags "$(LDFLAGS)" ./cmd/example-tests/...
$(BUILD_DIR)/$(BINARY_NAME):
	@echo "Building statically linked example-tests binary..."
	@mkdir -p $(BUILD_DIR)
	GO_COMPLIANCE_POLICY=exempt_all CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -ldflags="$(LDFLAGS) -s -w" \
	-o $(BUILD_DIR)/$(BINARY_NAME) "./main.go"

#framework-tests:
#	# GO_COMPLIANCE_POLICY="exempt_all" must only be used for test related binaries.
#	# It prevents various FIPS compliance policies from being applied to this compilation.
#	# Do not set globally.
#	GO_COMPLIANCE_POLICY="exempt_all" go build -ldflags "$(LDFLAGS)" ./cmd/framework-tests/...

test: unit
#test: unit integration

unit:
	go test ./...

#integration: build
#	./framework-tests run-suite framework

lint:
	./hack/go-lint.sh run ./...

clean:
	rm -f $(BUILD_DIR)
#	rm -f example-tests framework-tests
