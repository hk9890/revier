# `mise` is the canonical surface (see docs/TESTING.md). This makefile covers
# the subset that must work without mise installed. There is deliberately no
# `lint` target: golangci-lint is pinned in .mise.toml, and a recipe-less
# .PHONY entry would make `make lint` a silent no-op.
.PHONY: all build test test-integration vet fmt clean tidy

all: build

build:
	@go build -o bin/revier ./cmd/revier

test:
	@go test -race ./...

test-integration:
	@go test -race -tags=integration ./...

vet:
	@go vet ./...

fmt:
	@go fmt ./...

tidy:
	@go mod tidy

clean:
	@rm -rf bin coverage.out
