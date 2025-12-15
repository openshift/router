# Include bindata targets
include bindata.mk

# Build extension binary
.PHONY: build
build: bindata
	go build -o router ./cmd

# Run tests
.PHONY: test
test:
	go test ./...

# List all tests
.PHONY: list
list: build
	./router list

# Clean generated files
.PHONY: clean
clean: clean-bindata
	rm -f router

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  bindata     - Generate bindata.go from test/testdata"
	@echo "  build       - Build extension binary (includes bindata)"
	@echo "  test        - Run Go tests"
	@echo "  list        - List all available tests"
	@echo "  clean       - Remove generated files"
