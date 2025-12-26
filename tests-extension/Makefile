# Include bindata targets
include bindata.mk

# Binary name and output directory
BINARY := bin/router-tests-ext

# Build extension binary
.PHONY: build
build: bindata
	@echo "Building extension binary..."
	@mkdir -p bin
	go build -o $(BINARY) ./cmd
	@echo "Binary built successfully at $(BINARY)"

# Clean generated files
.PHONY: clean
clean:
	@echo "Cleaning binaries..."
	@rm -f $(BINARY)

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  bindata     - Generate bindata.go from test/testdata"
	@echo "  build       - Build extension binary (includes bindata)"
	@echo "  clean       - Remove extension binary"
