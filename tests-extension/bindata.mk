# Bindata generation for testdata files
# This file is included by the main Makefile

# Testdata path
TESTDATA_PATH := test/testdata

# go-bindata tool path
GOPATH ?= $(shell go env GOPATH)
GO_BINDATA := $(GOPATH)/bin/go-bindata

# Install go-bindata if not present
$(GO_BINDATA):
	@echo "Installing go-bindata to $(GO_BINDATA)..."
	@go install github.com/go-bindata/go-bindata/v3/go-bindata@latest
	@echo "go-bindata installed successfully"

# Generate bindata.go from testdata directory
.PHONY: bindata
bindata: $(GO_BINDATA) $(TESTDATA_PATH)/bindata.go

$(TESTDATA_PATH)/bindata.go: $(GO_BINDATA) $(shell find $(TESTDATA_PATH) -type f -not -name 'bindata.go' 2>/dev/null)
	@echo "Generating bindata from $(TESTDATA_PATH)..."
	@mkdir -p $(@D)
	$(GO_BINDATA) -nocompress -nometadata \
		-pkg testdata -o $@ -prefix "test" $(TESTDATA_PATH)/...
	@gofmt -s -w $@
	@echo "Bindata generated successfully at $@"

.PHONY: clean-bindata
clean-bindata:
	rm -f $(TESTDATA_PATH)/bindata.go
