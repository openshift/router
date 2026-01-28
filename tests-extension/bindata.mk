# Bindata generation for testdata files

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
bindata: clean-bindata $(GO_BINDATA)
	@echo "Generating bindata from $(TESTDATA_PATH)..."
	@mkdir -p $(TESTDATA_PATH)
	$(GO_BINDATA) -nocompress -nometadata \
		-pkg testdata -o $(TESTDATA_PATH)/bindata.go -prefix "test" $(TESTDATA_PATH)/...
	@gofmt -s -w $(TESTDATA_PATH)/bindata.go
	@echo "Bindata generated successfully at $(TESTDATA_PATH)/bindata.go"

.PHONY: clean-bindata
clean-bindata:
	@echo "Cleaning bindata..."
	@rm -f $(TESTDATA_PATH)/bindata.go
