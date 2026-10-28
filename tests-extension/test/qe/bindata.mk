TESTDATA_PATH := testdata
GO_BINDATA := GOFLAGS= go run github.com/go-bindata/go-bindata/v3/go-bindata@v3.1.3
BINDATA_OUTPUT ?= bindata.go

.PHONY: update-bindata
update-bindata:
	@echo "Generating bindata..."
	@mkdir -p $(TESTDATA_PATH)
	$(GO_BINDATA) -nocompress -nometadata \
		-pkg qe -o $(BINDATA_OUTPUT) -prefix "testdata" $(TESTDATA_PATH)/...
	@gofmt -s -w $(BINDATA_OUTPUT)
	@echo "✅ Bindata generated successfully"

.PHONY: verify-bindata
verify-bindata:
	@echo "Verifying bindata is up to date..."
	@git ls-files --error-unmatch bindata.go >/dev/null && test -f bindata.go || \
		{ echo "❌ bindata.go must exist and be tracked"; exit 1; }
	@temporary=$$(mktemp); trap 'rm -f "$$temporary"' EXIT HUP INT TERM; \
		$(MAKE) -f bindata.mk update-bindata BINDATA_OUTPUT="$$temporary" && \
		cmp -s bindata.go "$$temporary" || \
		{ echo "❌ Bindata is out of date; run make update-bindata in tests-extension"; exit 1; }
	@echo "✅ Bindata is up to date"

.PHONY: bindata
bindata: update-bindata
