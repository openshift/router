TESTDATA_PATH := testdata
GOPATH ?= $(shell go env GOPATH)
GO_BINDATA := $(GOPATH)/bin/go-bindata

$(GO_BINDATA):
	@echo "Installing go-bindata..."
	@GOFLAGS= go install github.com/go-bindata/go-bindata/v3/go-bindata@v3.1.3

.PHONY: update-bindata
update-bindata: $(GO_BINDATA)
	@echo "Generating bindata..."
	@mkdir -p $(TESTDATA_PATH)
	$(GO_BINDATA) -nocompress -nometadata \
		-pkg qe -o bindata.go -prefix "testdata" $(TESTDATA_PATH)/...
	@gofmt -s -w bindata.go
	@echo "✅ Bindata generated successfully"

.PHONY: verify-bindata
verify-bindata: update-bindata
	@echo "Verifying bindata is up to date..."
	git diff --exit-code bindata.go || (echo "❌ Bindata is out of date" && exit 1)
	@echo "✅ Bindata is up to date"

.PHONY: bindata
bindata: clean-bindata update-bindata

.PHONY: clean-bindata
clean-bindata:
	@rm -f bindata.go
