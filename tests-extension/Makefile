BINARY := bin/router-tests-ext

.PHONY: build
build:
	@echo "Building extension binary..."
	@mkdir -p bin
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o $(BINARY) ./cmd
	@echo "✅ Binary built: $(BINARY)"

.PHONY: clean
clean:
	@rm -f $(BINARY)

.PHONY: update-bindata verify-bindata verify
update-bindata verify-bindata:
	@cd test/qe && $(MAKE) -f bindata.mk $@

verify: verify-bindata

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build  - Build extension binary"
	@echo "  clean  - Remove the extension binary"
	@echo "  update-bindata - Regenerate committed QE fixtures"
	@echo "  verify-bindata - Check committed QE fixtures without modifying them"
