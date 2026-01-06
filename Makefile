.PHONY: build build-linux build-all test test-unit test-integration test-e2e test-coverage lint clean install docs

# Build variables
BINARY_NAME=dcx
BUILD_DIR=bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-s -w -X github.com/griffithind/dcx/internal/cli.Version=$(VERSION)"

# Default target
all: build

# Build the binary for current platform
build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/dcx

# Build Linux binaries for container deployment (SSH agent proxy)
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/dcx
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/dcx

# Build all binaries (host + Linux for containers)
build-all: build build-linux

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

# Run end-to-end tests (requires Docker)
test-e2e:
	go test -v -tags=e2e ./test/e2e/...

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
	@echo "  build           - Build the dcx binary for current platform"
	@echo "  build-linux     - Build Linux binaries (amd64/arm64) for containers"
	@echo "  build-all       - Build all binaries (host + Linux)"
	@echo "  install         - Install dcx to GOPATH/bin"
	@echo "  test            - Run unit tests"
	@echo "  test-unit       - Run unit tests with verbose output"
	@echo "  test-integration- Run integration tests (requires Docker)"
	@echo "  test-e2e        - Run end-to-end tests (requires Docker)"
	@echo "  test-coverage   - Run tests with coverage report"
	@echo "  lint            - Run golangci-lint"
	@echo "  docs            - Generate API documentation"
	@echo "  clean           - Remove build artifacts"
	@echo "  deps            - Download and tidy dependencies"
	@echo "  fmt             - Format source code"
	@echo "  vet             - Run go vet"
