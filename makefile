# `mise` is the canonical surface (see docs/TESTING.md). This makefile covers
# the subset that must work without mise installed. There is deliberately no
# `lint` target: golangci-lint is pinned in .mise.toml, and a recipe-less
# .PHONY entry would make `make lint` a silent no-op.
.PHONY: all build test test-integration vet fmt clean tidy

# The stamp .mise.toml's build task makes; change both together.
PKG = github.com/hk9890/revier/internal/build
VERSION = $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT = $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

all: build

build:
	@go build -ldflags "$(LDFLAGS)" -o bin/revier ./cmd/revier

# The race detector needs cgo, which .mise.toml's CGO_ENABLED=0 turns off in
# a mise-activated shell.
test:
	@CGO_ENABLED=1 go test -race ./...

test-integration:
	@CGO_ENABLED=1 go test -race -tags=integration ./...

vet:
	@go vet ./... && go vet -tags=integration ./... && go vet -tags=live ./...

# The goimports version is the one .mise.toml pins; bump both together, or
# `make fmt` writes what `mise run fmt:check` refuses.
fmt:
	@go run golang.org/x/tools/cmd/goimports@v0.52.0 -local github.com/hk9890/revier -w cmd internal pkg scripts docs/user-guide

tidy:
	@go mod tidy

clean:
	@rm -rf bin coverage.out
