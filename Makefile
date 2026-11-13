.PHONY: help build test test-race test-coverage clean lint lint-fix security fmt fmt-check vet install-tools deps tidy tidy-check gate vuln release-dry-run

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
	@echo "Coverage summary:"
	$(GO) tool cover -func=$(COVERAGE_FILE)
	@echo ""
	@echo "Coverage percentage:"
	$(GO) tool cover -func=$(COVERAGE_FILE) | tail -1
	@echo ""
	@echo "Generating HTML coverage report..."
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

## test-coverage-func: Show detailed function coverage
test-coverage-func:
	@echo "Running tests with detailed function coverage..."
	$(GOTEST) -v -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	@echo "Detailed function coverage:"
	$(GO) tool cover -func=$(COVERAGE_FILE)

## test-coverage-html: Generate HTML coverage report only
test-coverage-html:
	@if [ ! -f $(COVERAGE_FILE) ]; then \
		echo "No coverage file found. Run 'make test-coverage' first."; \
		exit 1; \
	fi
	@echo "Generating HTML coverage report..."
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

## test-coverage-check: Check if coverage meets threshold
test-coverage-check:
	@echo "Running tests with coverage check..."
	$(GOTEST) -v -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	@echo "Checking coverage threshold..."
	@COVERAGE=$$($(GO) tool cover -func=$(COVERAGE_FILE) | tail -1 | awk '{print $$3}' | sed 's/%//'); \
	COVERAGE_INT=$$(echo "$$COVERAGE" | cut -d. -f1); \
	if [ $$COVERAGE_INT -lt 70 ]; then \
		echo "ERROR: Coverage $$COVERAGE% is below 70% threshold"; \
		exit 1; \
	else \
		echo "SUCCESS: Coverage $$COVERAGE% meets 70% threshold"; \
	fi

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

## fmt-check: Fail if any file needs gofmt (changes nothing)
fmt-check:
	@echo "Checking formatting..."
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then \
		echo "Not gofmt-formatted (run 'make fmt'):"; echo "$$out"; exit 1; \
	fi

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

## lint: Run golangci-lint using .golangci.yml
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
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

## vuln: Run govulncheck against the module (auto-installs if missing)
GOVULNCHECK_VERSION ?= latest
vuln:
	@echo "Running govulncheck..."
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		echo "  govulncheck not found, installing $(GOVULNCHECK_VERSION)..."; \
		go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION); \
	fi
	@govulncheck ./...

## release-dry-run: Build a snapshot release locally (no publish)
release-dry-run:
	@echo "Running goreleaser snapshot..."
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "  goreleaser not found. Install: brew install goreleaser/tap/goreleaser"; \
		exit 1; \
	fi
	@goreleaser release --snapshot --clean

##@ Dependencies

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download

## tidy: Tidy up go.mod and go.sum
tidy:
	@echo "Tidying up dependencies..."
	$(GO) mod tidy

## tidy-check: Fail if go.mod or go.sum are not tidy (changes nothing)
tidy-check:
	@echo "Checking go.mod and go.sum are tidy..."
	@$(GO) mod tidy -diff

## verify: Verify dependencies
verify:
	@echo "Verifying dependencies..."
	$(GO) mod verify

## install-tools: Install development tools
GOLANGCI_LINT_VERSION ?= v2.15.0
install-tools:
	@echo "Installing required development tools..."
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION)
	@echo ""
	@echo "Required tools installed!"
	@echo ""
	@echo "Optional tools (install manually if needed):"
	@echo "  - gosec:       go install github.com/securego/gosec/v2/cmd/gosec@latest"
	@echo "  - staticcheck: go install honnef.co/go/tools/cmd/staticcheck@latest"

##@ Coverage

## coverage: Alias for test-coverage
coverage: test-coverage

## coverage-open: Open HTML coverage report in browser
coverage-open: test-coverage-html
	@echo "Opening coverage report in browser..."
	@if command -v xdg-open >/dev/null 2>&1; then \
		xdg-open $(COVERAGE_HTML); \
	elif command -v open >/dev/null 2>&1; then \
		open $(COVERAGE_HTML); \
	else \
		echo "Please open $(COVERAGE_HTML) in your browser"; \
	fi

## coverage-clean: Clean only coverage files
coverage-clean:
	@echo "Cleaning coverage files..."
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)

##@ Cleanup

## clean: Clean build artifacts and test data
clean: coverage-clean
	@echo "Cleaning up..."
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

## gate: The one check that must pass before merge; CI runs exactly this
# Raises the open-file limit because bitcoind needs more than the macOS
# default. CI=true makes a missing bitcoind a failure instead of a skip.
COVERAGE_THRESHOLD ?= 70
gate: fmt-check tidy-check vet lint
	@echo "Running tests with race detector and coverage..."
	@ulimit -n 4096 2>/dev/null || true; \
	CI=true $(GOTEST) -v -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	@COVERAGE=$$($(GO) tool cover -func=$(COVERAGE_FILE) | tail -1 | awk '{print $$3}' | sed 's/%//'); \
	if [ $${COVERAGE%.*} -lt $(COVERAGE_THRESHOLD) ]; then \
		echo "ERROR: Coverage $$COVERAGE% is below $(COVERAGE_THRESHOLD)% threshold"; \
		exit 1; \
	fi; \
	echo "Coverage $$COVERAGE% meets $(COVERAGE_THRESHOLD)% threshold"
	@echo ""
	@echo "gate passed!"

## pre-commit: Run pre-commit checks (quick)
pre-commit: fmt vet lint test
	@echo ""
	@echo "Pre-commit checks passed!"

