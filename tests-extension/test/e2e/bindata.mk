# bindata.mk for embedding testdata files

BINDATA_PKG := testdata
BINDATA_OUT := testdata/bindata.go

.PHONY: update-bindata
update-bindata:
	@echo "Generating bindata for testdata files..."
	go-bindata \
		-nocompress \
		-nometadata \
		-prefix "testdata" \
		-pkg $(BINDATA_PKG) \
		-o testdata/bindata.go \
		testdata/...
	@echo "✅ Bindata generated successfully"

.PHONY: verify-bindata
verify-bindata: update-bindata
	@echo "Verifying bindata is up to date..."
	git diff --exit-code $(BINDATA_OUT) || (echo "❌ Bindata is out of date. Run 'make update-bindata'" && exit 1)
	@echo "✅ Bindata is up to date"

# Legacy alias for backward compatibility
.PHONY: bindata
bindata: update-bindata

.PHONY: clean-bindata
clean-bindata:
	@echo "Cleaning bindata..."
	@rm -f $(BINDATA_OUT)
