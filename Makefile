.PHONY: build build-linux build-all test test-unit test-integration test-e2e test-coverage lint clean install docs

# Build variables
BINARY_NAME=dcx
BUILD_DIR=bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-s -w -X github.com/griffithind/dcx/internal/version.Version=$(VERSION)"

# Default target
all: build

# Build Linux binaries, compress for embedding, then build host binary
# CGO_ENABLED=0 ensures static linking for compatibility with all Linux distros (including Alpine/musl)
# All builds now include embedded compressed Linux binaries for SSH agent forwarding
build: build-linux-compress
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/dcx

# Build and compress Linux binaries for embedding
# Uses small placeholders for Linux build to avoid recursive embedding
build-linux-compress:
	@mkdir -p $(BUILD_DIR) internal/ssh/bin
	@# Always use small placeholders for Linux build (avoids recursive embedding)
	@echo "placeholder" | gzip > internal/ssh/bin/$(BINARY_NAME)-linux-amd64.gz
	@echo "placeholder" | gzip > internal/ssh/bin/$(BINARY_NAME)-linux-arm64.gz
	@echo "Building Linux binaries (with placeholder embeds)..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/dcx
	@echo "Compressing for embedding..."
	gzip -c $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 > internal/ssh/bin/$(BINARY_NAME)-linux-amd64.gz
	gzip -c $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 > internal/ssh/bin/$(BINARY_NAME)-linux-arm64.gz

# Build Linux binaries only (for standalone distribution)
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/dcx

# Build all platform binaries for release (all include embedded Linux binaries)
build-release: build-linux-compress
	@echo "Building release binaries..."
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/dcx

# Build all binaries for current platform + release
build-all: build build-release

# Install to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/dcx

# Run all tests
test: test-unit

# Run unit tests
test-unit:
	go test -v -race ./internal/... ./pkg/...

# Run integration tests (requires Docker)
test-integration:
	go test -v -tags=integration ./test/integration/...

# Run end-to-end tests (requires Docker) with parallel execution
test-e2e:
	go test -v -tags=e2e -parallel=4 ./test/e2e/...

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

# Help target
help:
	@echo "Available targets:"
	@echo "  build              - Build dcx with embedded Linux binaries (default)"
	@echo "  build-linux        - Build Linux binaries only (for standalone distribution)"
	@echo "  build-release      - Build all platform binaries for release"
	@echo "  build-all          - Build all binaries (current platform + release)"
	@echo "  install            - Install dcx to GOPATH/bin"
	@echo "  test               - Run unit tests"
	@echo "  test-unit          - Run unit tests with verbose output"
	@echo "  test-integration   - Run integration tests (requires Docker)"
	@echo "  test-e2e           - Run end-to-end tests with parallel execution (requires Docker)"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  lint               - Run golangci-lint"
	@echo "  docs               - Generate API documentation"
	@echo "  clean              - Remove build artifacts"
	@echo "  deps               - Download and tidy dependencies"
	@echo "  fmt                - Format source code"
	@echo "  vet                - Run go vet"
