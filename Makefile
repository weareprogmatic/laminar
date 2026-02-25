.PHONY: all build build-lambda test coverage lint fmt clean install loc loc-no-tests help

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
        powershell -NoProfile -Command "(Get-Date -AsUTC).ToString('yyyy-MM-ddTHH:mm:ssZ')" 2>/dev/null || \
        echo "unknown")

# Binary extension — empty on Linux/macOS, .exe on Windows
GOEXE := $(shell go env GOEXE)

# Build flags
LDFLAGS := -X github.com/weareprogmatic/laminar/internal/version.Version=$(VERSION) \
           -X github.com/weareprogmatic/laminar/internal/version.Commit=$(COMMIT) \
           -X github.com/weareprogmatic/laminar/internal/version.Date=$(DATE)

# Default target
all: fmt lint test build

# Build binaries to artifacts/ directory
build:
	@echo "Building laminar $(VERSION)..."
	@mkdir -p artifacts
	go build -ldflags "$(LDFLAGS)" -o artifacts/laminar$(GOEXE) ./cmd/laminar
	@echo "Building example hello binary..."
	cd examples/hello && go build -o ../../artifacts/hello$(GOEXE) .
	@echo "Build complete: artifacts/laminar$(GOEXE), artifacts/hello$(GOEXE)"

# Build only the hello Lambda example
build-lambda:
	@echo "Building hello Lambda..."
	@mkdir -p artifacts
	cd examples/hello && go build -o ../../artifacts/hello$(GOEXE) .
	@echo "Build complete: artifacts/hello$(GOEXE)"

# Run tests
test:
	@echo "Running tests..."
	go test -v -race ./cmd/... ./internal/...

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	@mkdir -p artifacts
	go test -race -coverprofile=artifacts/coverage.out -coverpkg=./internal/... ./internal/...
	@echo ""
	@echo "Coverage by function:"
	@go tool cover -func=artifacts/coverage.out | grep -v "^total:"
	@echo ""
	@go tool cover -func=artifacts/coverage.out | grep "^total:"
	@echo ""
	@echo "To view detailed coverage in browser:"
	@echo "  go tool cover -html=artifacts/coverage.out"

# Run linter
lint:
	@echo "Running linter..."
	@command -v golangci-lint > /dev/null 2>&1 || (echo "golangci-lint not found. Install from https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@command -v goimports > /dev/null 2>&1 && goimports -w . || echo "goimports not found, skipping (install with: go install golang.org/x/tools/cmd/goimports@latest)"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf artifacts/ 2>/dev/null || (if exist artifacts rd /s /q artifacts)
	go clean

# Install to GOPATH/bin
install:
	@echo "Installing laminar..."
	go install -ldflags "$(LDFLAGS)" ./cmd/laminar

# Count lines of code
loc:
	@echo "Lines of code:"
	@cloc --exclude-dir=artifacts,vendor,examples . 2>/dev/null || \
		echo "cloc not found - install with: brew install cloc / apt install cloc / choco install cloc"

# Count lines of code excluding tests
loc-no-tests:
	@echo "Lines of code (excluding tests):"
	@cloc --exclude-dir=artifacts,vendor,examples --not-match-f='_test\.go$$' . 2>/dev/null || \
		echo "cloc not found - install with: brew install cloc / apt install cloc / choco install cloc"

# Show help
help:
	@echo "Laminar Makefile targets:"
	@echo "  all          - Format, lint, test, and build (default)"
	@echo "  build        - Build binaries to artifacts/"
	@echo "  build-lambda - Build only the hello Lambda to artifacts/"
	@echo "  test         - Run tests with race detector"
	@echo "  coverage     - Run tests with coverage report"
	@echo "  lint         - Run golangci-lint"
	@echo "  fmt          - Format code with go fmt and goimports"
	@echo "  clean        - Remove build artifacts"
	@echo "  install      - Install laminar to GOPATH/bin"
	@echo "  loc          - Count lines of code"
	@echo "  loc-no-tests - Count lines of code excluding tests"
	@echo "  help         - Show this help message"

