.PHONY: help build test test-unit test-integration test-integration-local test-coverage test-race clean fmt vet lint install pre-commit release-snapshot localstack-up localstack-down localstack-logs

# Variables
BINARY_NAME=nic
CMD_DIR=./cmd/nic
PKG_DIRS=$(shell go list ./... | grep -v /vendor/)
GO_FILES=$(shell find . -type f -name '*.go' -not -path "./vendor/*")

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
	go build $(LDFLAGS) -o $(BINARY_NAME) $(CMD_DIR)
	@echo "Built $(BINARY_NAME) successfully"

build-all: ## Build binaries for all platforms
	@echo "Building for all platforms..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 $(CMD_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-arm64 $(CMD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-amd64 $(CMD_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-arm64 $(CMD_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe $(CMD_DIR)
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-arm64.exe $(CMD_DIR)
	@echo "Built all platform binaries successfully"

install: ## Install the binary to $GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	go install $(LDFLAGS) $(CMD_DIR)
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

lint: ## Run golint
	@echo "Running golint..."
	@which golint > /dev/null || (echo "Installing golint..." && go install golang.org/x/lint/golint@latest)
	golint -set_exit_status $(PKG_DIRS)
	@echo "Lint passed successfully"

test: test-unit ## Run unit tests (default)

test-unit: ## Run unit tests only
	@echo "Running unit tests..."
	go test -v -short $(PKG_DIRS)
	@echo "Unit tests passed successfully"

test-integration: ## Run integration tests (uses testcontainers, requires Docker)
	@echo "Running integration tests with testcontainers..."
	@which docker > /dev/null || (echo "Error: Docker is not installed or not running" && exit 1)
	go test -v -tags=integration ./pkg/provider/aws -timeout 30m
	@echo "Integration tests passed successfully"

test-integration-local: localstack-up ## Run integration tests against local docker-compose LocalStack
	@echo "Running integration tests against LocalStack..."
	@echo "Waiting for LocalStack to be ready..."
	@sleep 5
	AWS_ENDPOINT_URL=http://localhost:4566 go test -v -tags=integration ./pkg/provider/aws -timeout 30m
	@echo "Integration tests passed successfully"

test-all: ## Run all tests (unit + integration)
	@echo "Running all tests..."
	$(MAKE) test-unit
	$(MAKE) test-integration
	@echo "All tests passed successfully"

localstack-up: ## Start LocalStack using docker-compose
	@echo "Starting LocalStack..."
	@which docker-compose > /dev/null || which docker > /dev/null || (echo "Error: docker-compose or docker is not installed" && exit 1)
	@if command -v docker-compose > /dev/null 2>&1; then \
		docker-compose -f docker-compose.test.yml up -d; \
	else \
		docker compose -f docker-compose.test.yml up -d; \
	fi
	@echo "Waiting for LocalStack to be healthy..."
	@timeout 30 sh -c 'until curl -sf http://localhost:4566/_localstack/health > /dev/null 2>&1; do sleep 1; done' || (echo "LocalStack failed to start" && exit 1)
	@echo "LocalStack is ready!"

localstack-down: ## Stop LocalStack
	@echo "Stopping LocalStack..."
	@if command -v docker-compose > /dev/null 2>&1; then \
		docker-compose -f docker-compose.test.yml down; \
	else \
		docker compose -f docker-compose.test.yml down; \
	fi
	@echo "LocalStack stopped"

localstack-logs: ## Show LocalStack logs
	@if command -v docker-compose > /dev/null 2>&1; then \
		docker-compose -f docker-compose.test.yml logs -f localstack; \
	else \
		docker compose -f docker-compose.test.yml logs -f localstack; \
	fi

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
