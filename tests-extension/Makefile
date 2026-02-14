BINARY := bin/router-tests-ext

.PHONY: build
build:
	@echo "Building extension binary..."
	@cd test/e2e && $(MAKE) -f bindata.mk update-bindata
	@mkdir -p bin
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -mod=mod -o $(BINARY) ./cmd
	@echo "✅ Binary built: $(BINARY)"

.PHONY: clean
clean:
	@rm -f $(BINARY)
	@cd test/e2e && $(MAKE) -f bindata.mk clean-bindata

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build  - Build extension binary"
	@echo "  clean  - Remove binaries and bindata"
