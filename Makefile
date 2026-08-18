.PHONY: help build test test-unit test-integration test-coverage test-race clean fmt vet lint vuln install pre-commit release-snapshot argdown

# Variables
BINARY_NAME=nic
CMD_DIR=./cmd/nic
PKG_DIRS=$(shell go list ./... | grep -v /vendor/)
GO_FILES=$(shell find . -type f -name '*.go' -not -path "./vendor/*")
ARGDOWN_CONFIG=docs/adr/storage-strategy/argdown.config.js
ARGDOWN_OUT=$(dir $(ARGDOWN_CONFIG))
# process names defined in ARGDOWN_CONFIG
ARGDOWN_MAPS=$(shell node -e 'console.log(Object.keys(require("./$(ARGDOWN_CONFIG)").config.processes).join(" "))' 2>/dev/null)

# Build information
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

help: ## Display this help message
	@echo "Nebari Infrastructure Core - Makefile commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	CGO_ENABLED=0 go build -trimpath $(LDFLAGS) -o $(BINARY_NAME) $(CMD_DIR)
	@echo "Built $(BINARY_NAME) successfully"

build-all: ## Build binaries for all platforms
	@echo "Building for all platforms..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 $(CMD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath $(LDFLAGS) -o $(BINARY_NAME)-linux-arm64 $(CMD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath $(LDFLAGS) -o $(BINARY_NAME)-darwin-amd64 $(CMD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath $(LDFLAGS) -o $(BINARY_NAME)-darwin-arm64 $(CMD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe $(CMD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath $(LDFLAGS) -o $(BINARY_NAME)-windows-arm64.exe $(CMD_DIR)
	@echo "Built all platform binaries successfully"

install: ## Install the binary to $GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	CGO_ENABLED=0 go install -trimpath $(LDFLAGS) $(CMD_DIR)
	@echo "Installed $(BINARY_NAME) to $(shell go env GOPATH)/bin/$(BINARY_NAME)"

clean: ## Remove build artifacts
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME)-*
	rm -f coverage.out
	rm -f *.test
	@echo "Cleaned successfully"

fmt: ## Format Go code
	@echo "Running go fmt..."
	gofmt -s -w $(GO_FILES)
	@echo "Formatted successfully"

vet: ## Run go vet
	@echo "Running go vet..."
	go vet $(PKG_DIRS)
	@echo "Vet passed successfully"

lint: ## Run golangci-lint
	@echo "Running golangci-lint..."
	@which golangci-lint > /dev/null || (echo "Error: golangci-lint is not installed. See https://golangci-lint.run/welcome/install/" && exit 1)
	golangci-lint run
	@echo "Lint passed successfully"

vuln: ## Run govulncheck security gate (fails on reachable, fixable vulnerabilities)
	@echo "Running govulncheck gate..."
	@./scripts/govulncheck-gate.sh

test: test-unit ## Run unit tests (default)

test-unit: ## Run unit tests only
	@echo "Running unit tests..."
	go test -v -short $(PKG_DIRS)
	@echo "Unit tests passed successfully"

test-integration: ## Run integration tests (uses testcontainers, requires Docker)
	@echo "Running integration tests with testcontainers..."
	@which docker > /dev/null || (echo "Error: Docker is not installed or not running" && exit 1)
	go test -v -tags=integration ./pkg/providers/cluster/aws -timeout 30m
	@echo "Integration tests passed successfully"

test-all: ## Run all tests (unit + integration)
	@echo "Running all tests..."
	$(MAKE) test-unit
	$(MAKE) test-integration
	@echo "All tests passed successfully"

LOCAL_CONFIG?=./examples/local-config.yaml
REGEN_APPS?=

test-coverage: ## Run unit tests with coverage
	@echo "Running unit tests with coverage..."
	go test -v -short -coverprofile=coverage.out -covermode=atomic $(PKG_DIRS)
	go tool cover -func=coverage.out
	@echo "Coverage report generated: coverage.out"

test-race: ## Run unit tests with race detection
	@echo "Running unit tests with race detection..."
	go test -v -short -race $(PKG_DIRS)
	@echo "Race tests passed successfully"

check: fmt vet lint test ## Run all checks (fmt, vet, lint, test)
	@echo "All checks passed successfully"

pre-commit: ## Install pre-commit hooks
	@echo "Installing pre-commit hooks..."
	@which pre-commit > /dev/null || (echo "Error: pre-commit is not installed. Install with: pip install pre-commit" && exit 1)
	pre-commit install
	@echo "Pre-commit hooks installed successfully"

pre-commit-run: ## Run pre-commit hooks on all files
	@echo "Running pre-commit hooks..."
	pre-commit run --all-files

argdown: ## Render Argdown argument maps to SVG beside their .argdown source
	@echo "Rendering Argdown maps..."
	@which npx > /dev/null || (echo "Error: npx is not installed. Install Node.js." && exit 1)
	@# without the dot binary the CLI still reports success and writes only .dot
	@which dot > /dev/null || (echo "Error: graphviz is not installed. Install with: brew install graphviz" && exit 1)
	@# one process per map: an overview plus a map per argument direction
	for p in $(ARGDOWN_MAPS); do \
		npx -y @argdown/cli run $$p --config $(ARGDOWN_CONFIG) || exit 1; \
	done
	@# keep graphviz's width/height: an SVG with only a viewBox scales itself to
	@# the viewport, so browser zoom re-lays it out instead of magnifying it
	@# the CLI also drops its graphviz intermediate in ./dot; nothing consumes it
	@rm -rf dot
	@echo "SVGs written to $(ARGDOWN_OUT)"

release-snapshot: ## Create a snapshot release (local testing)
	@echo "Creating snapshot release..."
	@which goreleaser > /dev/null || (echo "Error: goreleaser is not installed. See https://goreleaser.com/install/" && exit 1)
	goreleaser release --snapshot --clean
	@echo "Snapshot release created successfully"

deps: ## Download Go dependencies
	@echo "Downloading dependencies..."
	go mod download
	go mod verify
	@echo "Dependencies downloaded successfully"

deps-update: ## Update Go dependencies
	@echo "Updating dependencies..."
	go get -u ./...
	go mod tidy
	@echo "Dependencies updated successfully"

.DEFAULT_GOAL := help
