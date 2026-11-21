.PHONY: all build build-agent build-linux release release-agent test test-unit test-e2e test-conformance test-all test-coverage lint clean install deadcode deps tidy verify fmt vet help

# Build variables
BINARY_NAME=dcx
AGENT_NAME=dcx-agent
BUILD_DIR=bin
# agent-embed.go embeds this directory; only the .gz files are generated.
EMBED_DIR=agentbin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-s -w -X github.com/griffithind/dcx/internal/version.Version=$(VERSION)"
RELEASE_LDFLAGS=-trimpath -ldflags "-s -w -X github.com/griffithind/dcx/internal/version.Version=$(VERSION)"

# Number of CPU cores for parallel test execution (works on macOS and Linux)
NCPU ?= $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)

# Default target
all: build

# Build agent binaries for Linux (to be embedded in main CLI)
build-agent:
	@mkdir -p $(BUILD_DIR)
	@echo "Building agent binaries..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(AGENT_NAME)-linux-amd64 ./cmd/dcx-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64 ./cmd/dcx-agent
	@echo "Compressing agent binaries for embedding..."
	gzip -n -c $(BUILD_DIR)/$(AGENT_NAME)-linux-amd64 > $(EMBED_DIR)/$(AGENT_NAME)-linux-amd64.gz
	gzip -n -c $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64 > $(EMBED_DIR)/$(AGENT_NAME)-linux-arm64.gz

# Build main CLI with embedded agent binaries
build: build-agent
	@echo "Building main CLI..."
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/dcx

# Build Linux CLI binaries (for standalone distribution)
build-linux: build-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/dcx

# Build optimized release binaries for all platforms
release: release-agent
	@echo "Building release binaries..."
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(RELEASE_LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(RELEASE_LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(RELEASE_LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(RELEASE_LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/dcx
	@echo "Generating checksums..."
	cd $(BUILD_DIR) && sha256sum $(BINARY_NAME)-darwin-* $(BINARY_NAME)-linux-* > checksums.txt

# Build optimized agent binaries for release
release-agent:
	@mkdir -p $(BUILD_DIR)
	@echo "Building agent binaries..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(RELEASE_LDFLAGS) -o $(BUILD_DIR)/$(AGENT_NAME)-linux-amd64 ./cmd/dcx-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(RELEASE_LDFLAGS) -o $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64 ./cmd/dcx-agent
	@echo "Compressing agent binaries for embedding..."
	gzip -n -c $(BUILD_DIR)/$(AGENT_NAME)-linux-amd64 > $(EMBED_DIR)/$(AGENT_NAME)-linux-amd64.gz
	gzip -n -c $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64 > $(EMBED_DIR)/$(AGENT_NAME)-linux-arm64.gz

# Install to $(go env GOPATH)/bin
install: build
	cp $(BUILD_DIR)/$(BINARY_NAME) $(shell go env GOPATH)/bin/

# Run all tests
test: test-unit

# Run unit tests (agent binaries are needed for go:embed)
test-unit: build-agent
	go test -v -race ./internal/...

# Run end-to-end tests (requires Docker, builds first). go test's default
# 10-minute timeout is too short for a cold runner pulling images. The tests
# mostly wait on Docker, so twice as many run as there are CPUs.
E2E_PARALLEL ?= $(shell expr $(NCPU) \* 2)
test-e2e: build
	go test -v -tags=e2e -timeout 30m -parallel=$(E2E_PARALLEL) ./test/e2e/...

# Run conformance tests (devcontainer spec compliance)
test-conformance:
	go test -v ./test/conformance/...

# Run all tests (unit, conformance, e2e)
test-all: test-unit test-conformance test-e2e

# Run tests with coverage
test-coverage: build-agent
	go test -coverprofile=coverage.out ./internal/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Pinned golangci-lint version (keep in sync with CI)
GOLANGCI_LINT_VERSION ?= v2.14.0

# Run linter (requires agent binaries for go:embed)
lint: build-agent
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f $(EMBED_DIR)/$(AGENT_NAME)-linux-*.gz
	rm -f coverage.out coverage.html

# Download dependencies
deps:
	go mod download

# Tidy go.mod/go.sum (modifies files)
tidy:
	go mod tidy

# Verify dependencies
verify:
	go mod verify

# Format code
fmt:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) fmt ./...

# Run go vet
vet: build-agent
	go vet ./...

# Pinned deadcode version (keep in sync with CI)
DEADCODE_VERSION ?= v0.52.0

# Find dead (unused) code; fails when any is found (deadcode itself exits 0)
deadcode: build-agent
	@out="$$(go run golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION) -test -filter='github.com/griffithind/dcx/(cmd|internal)' ./...)"; \
	if [ -n "$$out" ]; then echo "$$out"; echo "dead code found"; exit 1; fi

# Help target
help:
	@echo "Available targets:"
	@echo "  build              - Build dcx with embedded agent binaries (default)"
	@echo "  build-agent        - Build agent binaries for Linux"
	@echo "  build-linux        - Build Linux CLI binaries"
	@echo "  release            - Build optimized release binaries for all platforms"
	@echo "  install            - Install dcx to GOPATH/bin"
	@echo "  test               - Run unit tests"
	@echo "  test-unit          - Run unit tests with verbose output"
	@echo "  test-e2e           - Run end-to-end tests (builds first, requires Docker)"
	@echo "  test-conformance   - Run conformance tests (spec compliance)"
	@echo "  test-all           - Run all tests (unit, conformance, e2e; builds first)"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  lint               - Run golangci-lint"
	@echo "  clean              - Remove build artifacts"
	@echo "  deps               - Download dependencies"
	@echo "  tidy               - go mod tidy (modifies go.mod/go.sum)"
	@echo "  fmt                - Format source code"
	@echo "  vet                - Run go vet"
	@echo "  deadcode           - Find unused code"
