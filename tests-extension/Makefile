# Binary name and output directory
BINARY := bin/router-tests-ext

# Build extension binary
.PHONY: build
build:
	@echo "Building extension binary..."
	@cd test/e2e && $(MAKE) -f bindata.mk update-bindata
	@mkdir -p bin
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o $(BINARY) ./cmd
	@echo "✅ Binary built successfully at $(BINARY)"

# Clean generated files
.PHONY: clean
clean:
	@echo "Cleaning binaries..."
	@rm -f $(BINARY)
	@cd test/e2e && $(MAKE) -f bindata.mk clean-bindata

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build       - Build extension binary (includes bindata)"
	@echo "  clean       - Remove extension binary and bindata"
