# Kitchen Printer Tap Makefile
#
# Usage:
#   make build       - Build the tapd binary
#   make install     - Install to system (requires root)
#   make test        - Run tests
#   make clean       - Remove build artifacts
#   make dev         - Build for development (with race detector)
#   make lint        - Run linter

# Build configuration
BINARY_NAME := tapd
BUILD_DIR := bin
CMD_DIR := cmd/tapd
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Go configuration
GO := go
GOFLAGS := -trimpath
GOTEST := $(GO) test

# Installation paths
PREFIX ?= /usr/local
BINDIR := $(PREFIX)/bin
SYSCONFDIR := /etc/kitchen-printer-tap
DATADIR := /var/lib/kitchen-printer-tap
SYSTEMDDIR := /etc/systemd/system

# Default target
.PHONY: all
all: build

# Build the binary
.PHONY: build
build: $(BUILD_DIR)/$(BINARY_NAME)

$(BUILD_DIR)/$(BINARY_NAME): $(shell find . -name '*.go' -type f)
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

# Build for development with race detector
.PHONY: dev
dev:
	@echo "Building with race detector..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -race $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME) (with race detector)"

# Build for ARM (Revolution Pi)
.PHONY: build-arm
build-arm:
	@echo "Building for ARM..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-arm ./$(CMD_DIR)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-arm"

# Build for ARM64
.PHONY: build-arm64
build-arm64:
	@echo "Building for ARM64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-arm64 ./$(CMD_DIR)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-arm64"

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	$(GOTEST) -v -race ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run linter
.PHONY: lint
lint:
	@echo "Running linter..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	golangci-lint run ./...

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Verify dependencies
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod verify

# Tidy dependencies
.PHONY: tidy
tidy:
	@echo "Tidying dependencies..."
	$(GO) mod tidy

# Install to system (requires root)
.PHONY: install
install: build
	@echo "Installing kitchen-printer-tap..."
	@if [ "$$(id -u)" -ne 0 ]; then echo "Error: install requires root"; exit 1; fi
	./scripts/install.sh

# Uninstall from system
.PHONY: uninstall
uninstall:
	@echo "Uninstalling kitchen-printer-tap..."
	@if [ "$$(id -u)" -ne 0 ]; then echo "Error: uninstall requires root"; exit 1; fi
	systemctl stop kitchen-printer-tap 2>/dev/null || true
	systemctl disable kitchen-printer-tap 2>/dev/null || true
	rm -f $(BINDIR)/$(BINARY_NAME)
	rm -f $(SYSTEMDDIR)/kitchen-printer-tap.service
	systemctl daemon-reload
	@echo "Uninstall complete. Config and data preserved in $(SYSCONFDIR) and $(DATADIR)"

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Run the binary locally (for development)
.PHONY: run
run: build
	@echo "Running tapd..."
	sudo $(BUILD_DIR)/$(BINARY_NAME) --config=configs/config.yaml

# Show version
.PHONY: version
version:
	@echo "Version: $(VERSION)"
	@echo "Build time: $(BUILD_TIME)"

# Generate go.sum
.PHONY: go-sum
go-sum:
	$(GO) mod download

# Verify build
.PHONY: verify
verify: deps lint test build
	@echo "Verification complete"

# Help
.PHONY: help
help:
	@echo "Kitchen Printer Tap Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Build targets:"
	@echo "  build       - Build the binary (default)"
	@echo "  build-arm   - Build for ARM (Revolution Pi)"
	@echo "  build-arm64 - Build for ARM64"
	@echo "  dev         - Build with race detector"
	@echo "  clean       - Remove build artifacts"
	@echo ""
	@echo "Test targets:"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  lint          - Run linter"
	@echo "  fmt           - Format code"
	@echo ""
	@echo "Install targets:"
	@echo "  install   - Install to system (requires root)"
	@echo "  uninstall - Remove from system (requires root)"
	@echo ""
	@echo "Other targets:"
	@echo "  deps    - Download dependencies"
	@echo "  tidy    - Tidy dependencies"
	@echo "  run     - Run locally (for development)"
	@echo "  version - Show version info"
	@echo "  verify  - Full verification (deps, lint, test, build)"
	@echo "  help    - Show this help"
