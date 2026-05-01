.PHONY: all build clean fmt fmt-check lint test cover vuln e2e install-tools snapshot release help ensure-github-app-vars

# =============================================================================
# Variables
# =============================================================================

BINARY := stacktower
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/stacktower-io/stacktower/pkg/buildinfo.Version=$(VERSION) \
           -X github.com/stacktower-io/stacktower/pkg/buildinfo.Commit=$(COMMIT) \
           -X github.com/stacktower-io/stacktower/pkg/buildinfo.Date=$(DATE) \
           -X github.com/stacktower-io/stacktower/pkg/buildinfo.GitHubAppClientID=$(STACKTOWER_GITHUB_APP_CLIENT_ID) \
           -X github.com/stacktower-io/stacktower/pkg/buildinfo.GitHubAppSlug=$(STACKTOWER_GITHUB_APP_SLUG)

# =============================================================================
# Default Target
# =============================================================================

all: check build

# =============================================================================
# Build Targets
# =============================================================================

build:
	@echo "Building CLI (bin/$(BINARY))..."
	@go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/stacktower

install:
	@echo "Installing CLI..."
	@go install -ldflags "$(LDFLAGS)" ./cmd/stacktower

ensure-github-app-vars:
	@test -n "$(STACKTOWER_GITHUB_APP_CLIENT_ID)" || (echo "ERROR: STACKTOWER_GITHUB_APP_CLIENT_ID is required"; exit 1)
	@test -n "$(STACKTOWER_GITHUB_APP_SLUG)" || (echo "ERROR: STACKTOWER_GITHUB_APP_SLUG is required"; exit 1)

clean:
	@rm -rf bin/ dist/ coverage.out output/ tmp/

# =============================================================================
# Quality Checks
# =============================================================================

check: fmt lint test vuln

fmt:
	@gofmt -s -w .
	@goimports -w -local github.com/stacktower-io/stacktower .

fmt-check:
	@echo "Checking formatting..."
	@test -z "$$(gofmt -l .)" || (echo "Files not formatted:"; gofmt -l .; exit 1)
	@test -z "$$(goimports -l -local github.com/stacktower-io/stacktower .)" || (echo "Imports not formatted:"; goimports -l -local github.com/stacktower-io/stacktower .; exit 1)
	@echo "Formatting OK"

lint:
	@golangci-lint run

test:
	@go test -race -timeout=2m ./...

cover:
	@go test -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out

vuln:
	@govulncheck ./...

install-tools:
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install golang.org/x/vuln/cmd/govulncheck@latest

# =============================================================================
# End-to-End Tests
# =============================================================================

e2e: build
	@./scripts/test_cli_e2e.sh all

e2e-test: build
	@./scripts/test_cli_e2e.sh test

e2e-real: build
	@./scripts/test_cli_e2e.sh real

e2e-parse: build
	@./scripts/test_cli_e2e.sh parse

# =============================================================================
# Release
# =============================================================================

snapshot: ensure-github-app-vars
	@goreleaser release --snapshot --clean --skip=publish

release: ensure-github-app-vars
	@goreleaser release --clean

# =============================================================================
# Help
# =============================================================================

help:
	@echo "Stacktower Makefile"
	@echo ""
	@echo "BUILDING:"
	@echo "  make build            - Build CLI binary (bin/stacktower)"
	@echo "  make install          - Install CLI to GOPATH"
	@echo ""
	@echo "QUALITY:"
	@echo "  make check            - Run all checks (fmt, lint, test, vuln)"
	@echo "  make fmt              - Format code"
	@echo "  make fmt-check        - Check formatting (CI-style, no writes)"
	@echo "  make lint             - Run golangci-lint"
	@echo "  make test             - Run tests"
	@echo "  make cover            - Run tests with coverage"
	@echo "  make vuln             - Check for vulnerabilities"
	@echo ""
	@echo "TESTING:"
	@echo "  make e2e              - Run all CLI end-to-end tests"
	@echo "  make e2e-test         - Run test examples"
	@echo "  make e2e-real         - Run real package examples"
	@echo "  make e2e-parse        - Run parse tests"
	@echo ""
	@echo "RELEASE:"
	@echo "  make snapshot         - Build release locally (no publish)"
	@echo "  make release          - Build and publish release"
	@echo ""
	@echo "OTHER:"
	@echo "  make clean            - Remove build artifacts"
	@echo "  make install-tools    - Install development tools"
	@echo "  make help             - Show this help"
