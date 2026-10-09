# Binary settings
BINARY_NAME := mitto
MAIN_PACKAGE := ./cmd/mittodrop

# Detect development environment OS & Architecture via Go (supports Windows, Linux, Darwin, x64, arm64)
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Output binary file naming based on target OS
ifeq ($(GOOS),windows)
	BINARY := $(BINARY_NAME).exe
else
	BINARY := $(BINARY_NAME)
endif

# Output build directory
BUILD_DIR := bin

# Version metadata injected at compile time
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")

DATE    ?= $(shell git log -1 --format=%cI)

LDFLAGS := -s -w \
	-X mittodrop/internal/version.Version=$(VERSION) \
	-X mittodrop/internal/version.Commit=$(COMMIT) \
	-X mittodrop/internal/version.Date=$(DATE)

.PHONY: all build test test-verbose test-internals test-coverage fmt lint tidy clean release-snapshot release help

all: build

## build: Build binary for development host OS and architecture (GOOS/GOARCH)
build:
	@echo "Building $(BINARY) for $(GOOS)/$(GOARCH)..."
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) $(MAIN_PACKAGE)
	@echo "Binary created at $(BUILD_DIR)/$(BINARY)"

## test: Run complete test suite sequentially (prevents local port conflicts)
test:
	go test ./internal/... ./tests/integration/... ./cmd/app/... -p 1 -count=1

## test-verbose: Run complete test suite with verbose output
test-verbose:
	go test ./internal/... ./tests/integration/... ./cmd/app/... -p 1 -v -count=1

## test-internals: Run tests for all internal packages only
test-internals:
	go test ./internal/... -count=1

## test-coverage: Run test suite with coverage report and generate HTML
test-coverage:
	go test ./internal/... -p 1 -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated at coverage.html"

## fmt: Format all Go code in the project
fmt:
	go fmt ./...

## lint: Run go vet linter
lint:
	go vet ./...

## tidy: Tidy and verify module dependencies
tidy:
	go mod tidy
	go mod verify

## clean: Remove compiled binaries and test coverage artifacts
clean:
	go clean
	-rm -rf $(BUILD_DIR) dist coverage.out coverage.html 2>/dev/null || del /q /f $(BUILD_DIR)\* coverage.out coverage.html 2>nul || true

## release-snapshot: Build all release artifacts locally via GoReleaser (dry-run without publishing)
release-snapshot:
	goreleaser release --snapshot --clean

## release: Publish production release via GoReleaser (requires GITHUB_TOKEN and git tag)
release:
	goreleaser release --clean

## help: Show available Makefile commands
help:
	@echo "mittodrop Makefile commands:"
	@echo "  make build          - Build binary for host OS and architecture"
	@echo "  make test           - Run all tests sequentially"
	@echo "  make test-verbose   - Run all tests with verbose output"
	@echo "  make test-internals - Run internal module tests only"
	@echo "  make test-coverage  - Run tests and generate coverage.html"
	@echo "  make fmt            - Format all Go source files"
	@echo "  make lint           - Run linter (golangci-lint or go vet)"
	@echo "  make tidy             - Tidy and verify go.mod and go.sum"
	@echo "  make clean            - Remove build and test output files"
	@echo "  make release-snapshot - Build multiplatform release packages locally via GoReleaser"
	@echo "  make release          - Publish GitHub release with changelog via GoReleaser"
