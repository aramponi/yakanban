BINARY      := yakanban
MODULE      := github.com/aramponi/yakanban
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null)
LDFLAGS     := -s -w -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Commit=$(COMMIT)
GH_EXT_DIR  := $(HOME)/.local/share/gh/extensions/gh-$(BINARY)

.PHONY: all build test check fmt vet lint install gh-extension clean cross

all: check build

## build: static binary in ./bin
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/$(BINARY)

## test: run the whole suite with the race detector
test:
	go test -race ./...

## check: formatting, vet, lint and tests
check: fmt vet lint test

fmt:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "run gofmt -w ." && exit 1)

vet:
	go vet ./...

lint:
	golangci-lint run ./...

## install: put the binary in GOBIN
install:
	CGO_ENABLED=0 go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/$(BINARY)

## gh-extension: install the same binary as a GitHub CLI extension (gh yakanban ...)
gh-extension:
	@mkdir -p $(GH_EXT_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(GH_EXT_DIR)/gh-$(BINARY) ./cmd/$(BINARY)
	@echo "installed: gh $(BINARY) --help"

## cross: static binaries for macOS, Linux and Windows
cross:
	@set -e; for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64; do \
		os=$${target%/*}; arch=$${target#*/}; ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/$(BINARY)_$${os}_$${arch}$$ext ./cmd/$(BINARY); \
	done

clean:
	rm -rf bin dist
