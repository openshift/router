.PHONY: all build check

all: build

build:
	go build ./cmd/openshift-router

check:
	go test -race ./...
