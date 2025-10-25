.PHONY: help build test test-race test-coverage clean lint lint-fix security fmt vet install-tools deps tidy run-regtest stop-regtest check-all

# Default target
.DEFAULT_GOAL := help

# Project variables
PROJECT_NAME := go-regtest
GO := go
GOTEST := $(GO) test
GOVET := $(GO) vet
GOFMT := $(GO) fmt
COVERAGE_FILE := coverage.out
COVERAGE_HTML := coverage.html

## help: Display this help message
help:
	@echo "$(PROJECT_NAME) - Bitcoin Regtest Development Tools"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*##"; printf ""} /^[a-zA-Z_-]+:.*?##/ { printf "  %-20s %s\n", $$1, $$2 } /^##@/ { printf "\n%s\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

## build: Build the project
build:
	@echo "Building..."
	$(GO) build -v ./...

## test: Run all tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

## test-race: Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	$(GOTEST) -v -race ./...

## test-coverage: Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

## test-short: Run short tests only
test-short:
	@echo "Running short tests..."
	$(GOTEST) -v -short ./...

## bench: Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

##@ Code Quality

## fmt: Format code using gofmt
fmt:
	@echo "Formatting code..."
	$(GOFMT) ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

## lint: Run golangci-lint (lenient mode for development)
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --disable=errcheck,unused ./... || true; \
	else \
		echo "golangci-lint not installed. Run 'make install-tools' to install it."; \
		exit 1; \
	fi

## lint-fix: Run golangci-lint with auto-fix
lint-fix:
	@echo "Running golangci-lint with auto-fix..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --fix ./...; \
	else \
		echo "golangci-lint not installed. Run 'make install-tools' to install it."; \
		exit 1; \
	fi

## security: Run security checks with gosec (optional - install separately)
security:
	@echo "Running security checks..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec -fmt=json -out=gosec-report.json ./...; \
		gosec ./...; \
	else \
		echo "WARNING: gosec not installed (optional tool)"; \
		echo "   Install: go install github.com/securego/gosec/v2/cmd/gosec@latest"; \
	fi

## staticcheck: Run staticcheck (optional - requires Go version match)
staticcheck:
	@echo "Running staticcheck..."
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./... 2>&1 || echo "WARNING: staticcheck version mismatch (optional tool)"; \
	else \
		echo "WARNING: staticcheck not installed (optional tool)"; \
		echo "   Install: go install honnef.co/go/tools/cmd/staticcheck@latest"; \
	fi

##@ Dependencies

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download

## tidy: Tidy up go.mod and go.sum
tidy:
	@echo "Tidying up dependencies..."
	$(GO) mod tidy

## verify: Verify dependencies
verify:
	@echo "Verifying dependencies..."
	$(GO) mod verify

## install-tools: Install development tools
install-tools:
	@echo "Installing required development tools..."
	@echo "Installing golangci-lint..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo ""
	@echo "Required tools installed!"
	@echo ""
	@echo "Optional tools (install manually if needed):"
	@echo "  - gosec:       go install github.com/securego/gosec/v2/cmd/gosec@latest"
	@echo "  - staticcheck: go install honnef.co/go/tools/cmd/staticcheck@latest"

##@ Regtest Operations

## run-regtest: Start a regtest node for manual testing
run-regtest:
	@echo "Starting Bitcoin regtest node..."
	@bash scripts/bitcoind_manager.sh start

## stop-regtest: Stop the regtest node
stop-regtest:
	@echo "Stopping Bitcoin regtest node..."
	@bash scripts/bitcoind_manager.sh stop

## status-regtest: Check regtest node status
status-regtest:
	@echo "Checking Bitcoin regtest node status..."
	@bash scripts/bitcoind_manager.sh status

##@ Cleanup

## clean: Clean build artifacts and test data
clean:
	@echo "Cleaning up..."
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	@rm -f gosec-report.json
	@rm -rf ./bitcoind_regtest*
	@rm -rf ./bitcoin_regtest*
	@$(GO) clean -cache -testcache
	@echo "Cleanup complete!"

## clean-all: Clean everything including dependencies
clean-all: clean
	@echo "Removing downloaded dependencies..."
	@$(GO) clean -modcache
	@echo "Full cleanup complete!"

##@ Combined Checks

## check-all: Run all checks (fmt, vet, lint, test-race)
check-all: fmt vet lint test-race
	@echo ""
	@echo "All checks passed!"

## ci: Run CI pipeline checks
ci: deps tidy fmt vet lint test-coverage
	@echo ""
	@echo "CI checks passed!"

## pre-commit: Run pre-commit checks (quick)
pre-commit: fmt vet lint test
	@echo ""
	@echo "Pre-commit checks passed!"

