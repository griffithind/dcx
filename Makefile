.PHONY: build build-agent build-linux release release-agent test test-unit test-integration test-e2e test-conformance test-all test-coverage lint clean install docs deadcode

# Build variables
BINARY_NAME=dcx
AGENT_NAME=dcx-agent
BUILD_DIR=bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-s -w -X github.com/griffithind/dcx/internal/version.Version=$(VERSION)"
RELEASE_LDFLAGS=-ldflags "-s -w -trimpath -X github.com/griffithind/dcx/internal/version.Version=$(VERSION)"

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
	gzip -c $(BUILD_DIR)/$(AGENT_NAME)-linux-amd64 > $(BUILD_DIR)/$(AGENT_NAME)-linux-amd64.gz
	gzip -c $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64 > $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64.gz

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
	cd $(BUILD_DIR) && sha256sum $(BINARY_NAME)-* > checksums.txt

# Build optimized agent binaries for release
release-agent:
	@mkdir -p $(BUILD_DIR)
	@echo "Building agent binaries..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(RELEASE_LDFLAGS) -o $(BUILD_DIR)/$(AGENT_NAME)-linux-amd64 ./cmd/dcx-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(RELEASE_LDFLAGS) -o $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64 ./cmd/dcx-agent
	@echo "Compressing agent binaries for embedding..."
	gzip -c $(BUILD_DIR)/$(AGENT_NAME)-linux-amd64 > $(BUILD_DIR)/$(AGENT_NAME)-linux-amd64.gz
	gzip -c $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64 > $(BUILD_DIR)/$(AGENT_NAME)-linux-arm64.gz

# Install to GOPATH/bin
install: build
	cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/

# Run all tests
test: test-unit

# Run unit tests
test-unit:
	go test -v -race ./internal/... ./pkg/...

# Run integration tests (requires Docker)
test-integration:
	go test -v -tags=integration ./test/integration/...

# Run end-to-end tests (requires Docker, builds first)
test-e2e: build
	go test -v -tags=e2e -parallel=$(NCPU) ./test/e2e/...

# Run conformance tests (devcontainer spec compliance)
test-conformance:
	go test -v ./test/conformance/...

# Run all tests (unit, conformance, e2e)
test-all: test-unit test-conformance test-e2e

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./internal/... ./pkg/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run linter
lint:
	golangci-lint run ./...

# Generate documentation
docs:
	go doc -all ./internal/... > docs/api/godoc.txt

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Download dependencies
deps:
	go mod download
	go mod tidy

# Verify dependencies
verify:
	go mod verify

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Find dead (unused) code
deadcode:
	go run golang.org/x/tools/cmd/deadcode@latest ./...

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
	@echo "  test-integration   - Run integration tests (requires Docker)"
	@echo "  test-e2e           - Run end-to-end tests (builds first, requires Docker)"
	@echo "  test-conformance   - Run conformance tests (spec compliance)"
	@echo "  test-all           - Run all tests (unit, conformance, e2e; builds first)"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  lint               - Run golangci-lint"
	@echo "  docs               - Generate API documentation"
	@echo "  clean              - Remove build artifacts"
	@echo "  deps               - Download and tidy dependencies"
	@echo "  fmt                - Format source code"
	@echo "  vet                - Run go vet"
	@echo "  deadcode           - Find unused code"
