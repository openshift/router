BINARY := bin/router-tests-ext

.PHONY: build
build:
	@echo "Building extension binary..."
	@cd test/qe && $(MAKE) -f bindata.mk update-bindata
	@mkdir -p bin
	CGO_ENABLED=0 GO_COMPLIANCE_POLICY=exempt_all GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o $(BINARY) ./cmd
	@echo "✅ Binary built: $(BINARY)"

# update-metadata regenerates .openshift-tests-extension/openshift_payload_router.json,
# which records the name and lifecycle of every test the binary ships.  The
# underlying OTE command refuses to regenerate the file when a test has been
# renamed or removed without being declared, so run this after any change to a
# test name; see "Renaming or removing a test" in test/qe/README.md.
.PHONY: update-metadata
update-metadata: build
	./$(BINARY) update

# verify-metadata fails if the committed metadata is stale with respect to the
# tests in the binary.  It checks for untracked files as well as modifications,
# so that a newly generated metadata file is not mistaken for an up-to-date one.
.PHONY: verify-metadata
verify-metadata: update-metadata
	@if [ -n "$$(git status --porcelain -- .openshift-tests-extension)" ]; then \
		echo "ERROR: .openshift-tests-extension is out of date." >&2; \
		echo "Run 'make update-metadata' and commit the result." >&2; \
		git --no-pager diff -- .openshift-tests-extension; \
		exit 1; \
	fi

# Contribute verify-metadata to the aggregate verify target.  This is a rule of
# its own rather than an entry appended to a shared prerequisite list, because
# make collects the prerequisites of a target from every rule that names it.
# Other verify prerequisites can therefore be added without touching this line.
.PHONY: verify
verify: verify-metadata
	@cd test/qe && $(MAKE) -f bindata.mk verify-bindata

.PHONY: clean
clean:
	@rm -f $(BINARY)
	@cd test/qe && $(MAKE) -f bindata.mk clean-bindata

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build  - Build extension binary"
	@echo "  clean  - Remove binaries and bindata"
