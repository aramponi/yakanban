BINARY      := yakanban
MODULE      := github.com/aramponi/yakanban
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null)
LDFLAGS     := -s -w -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Commit=$(COMMIT)
GH_EXT_DIR  := $(HOME)/.local/share/gh/extensions/gh-$(BINARY)
# Keep in step with the version pinned in .github/workflows/ci.yml.
GOLANGCI_VERSION := v2.12.0

.PHONY: all build test check fmt vet lint install gh-extension clean cross site

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

## lint: golangci-lint, pinned to the version CI uses
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed. Install it with:"; \
		echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)"; \
		exit 1; }
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

## site: generate the landing page and llms.txt into site/public
# The terminal blocks are captured by running the binary against the real
# board, so this needs a token with the project scope — the same one the CLI
# uses. There is deliberately no offline fallback: a fallback would be a
# hand-written transcript, which is the mockup the page promises not to show.
site: build
	go run ./cmd/gensite -version "$(VERSION)"

clean:
	rm -rf bin dist site/public
