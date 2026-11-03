# `mise` is the canonical surface (see docs/TESTING.md). This makefile covers
# the subset that must work without mise installed. There is deliberately no
# `lint` target: golangci-lint is pinned in .mise.toml, and a recipe-less
# .PHONY entry would make `make lint` a silent no-op.
.PHONY: all build test test-integration vet fmt clean tidy

all: build

build:
	@go build -o bin/revier ./cmd/revier

# The race detector needs cgo, which .mise.toml's CGO_ENABLED=0 turns off in
# a mise-activated shell.
test:
	@CGO_ENABLED=1 go test -race ./...

test-integration:
	@CGO_ENABLED=1 go test -race -tags=integration ./...

vet:
	@go vet ./...

# The goimports version is the one .mise.toml pins; bump both together, or
# `make fmt` writes what `mise run fmt:check` refuses.
fmt:
	@go run golang.org/x/tools/cmd/goimports@v0.52.0 -local github.com/hk9890/revier -w cmd internal pkg scripts

tidy:
	@go mod tidy

clean:
	@rm -rf bin coverage.out
