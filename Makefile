# Cluso GraphDB Makefile
# Leverages Go's native tooling for testing, building, and profiling

.PHONY: help test test-verbose test-short test-race test-cover test-cover-html \
        bench bench-cpu bench-mem build build-all clean fmt vet lint \
        run-server run-cli run-tui install-tools mod-tidy mod-verify \
        integration-test api-test profile-cpu profile-mem

# Default target
.DEFAULT_GOAL := help

# Variables
BINARY_DIR := bin
DATA_DIR := data
COVERAGE_DIR := coverage
GO := go
GOFLAGS :=
TEST_TIMEOUT := 10m
BENCH_TIME := 5s

# Packages the CI test jobs cover. pkg/api + pkg/graphql were historically
# absent from this list, so CI never ran the REST/GraphQL test suites — that
# gap let a deterministically-red pkg/api test merge under green CI (#344,
# surfaced while fixing #224). They run in parallel with the others, so the
# slow pole (pkg/storage) still dominates wall-clock.
TEST_PKGS := ./pkg/storage/... ./pkg/lsm/... ./pkg/query/... \
	./pkg/algorithms/... ./pkg/parallel/... ./pkg/wal/... \
	./pkg/api/... ./pkg/graphql/...

# Race detector omits ./pkg/api/...: its server-spinning suite exceeds the 10m
# budget under -race -p 2 (a timeout, NOT a data race). pkg/graphql is
# race-clean and fast, so it stays in.
RACE_PKGS := ./pkg/storage/... ./pkg/lsm/... ./pkg/query/... \
	./pkg/algorithms/... ./pkg/parallel/... ./pkg/wal/... ./pkg/graphql/...

# Build variables
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)

## help: Display this help message
help:
	@echo "Cluso GraphDB - Makefile Targets"
	@echo "=================================="
	@echo ""
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

## test: Run all tests (excluding integration tests and tag-gated replication packages)
test:
	@echo "Running all tests..."
	$(GO) test -timeout $(TEST_TIMEOUT) $(TEST_PKGS)

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "Running tests (verbose)..."
	$(GO) test -v -timeout $(TEST_TIMEOUT) $(TEST_PKGS)

## test-short: Run tests in short mode (skip long-running tests)
# Core packages only — pkg/api's server-spinning suite is ~100s even under
# -short, which doesn't fit this target's 1m smoke budget.
test-short:
	@echo "Running short tests..."
	$(GO) test -short -timeout 1m \
		./pkg/storage/... ./pkg/lsm/... ./pkg/query/... \
		./pkg/algorithms/... ./pkg/parallel/... ./pkg/wal/...

## test-race: Run tests with race detector
# -p 2 caps package-parallelism to fit the race detector's memory footprint
# inside the GitHub-hosted runner's 7GB. Uses RACE_PKGS (omits pkg/api).
test-race:
	@echo "Running tests with race detector..."
	$(GO) test -race -p 2 -timeout $(TEST_TIMEOUT) $(RACE_PKGS)

## test-cover: Run tests with coverage analysis
# Core packages only — this runs on the ubuntu coverage job; keeping pkg/api
# (slow, server-spinning) off it avoids loading the exit-143-prone runner. The
# correctness gate for api/graphql is the macOS test-verbose job.
test-cover:
	@echo "Running tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	$(GO) test -cover -coverprofile=$(COVERAGE_DIR)/coverage.out \
		./pkg/storage/... ./pkg/lsm/... ./pkg/query/... \
		./pkg/algorithms/... ./pkg/parallel/... ./pkg/wal/...
	@echo ""
	@echo "Coverage Summary:"
	@$(GO) tool cover -func=$(COVERAGE_DIR)/coverage.out | tail -1

## test-cover-html: Generate HTML coverage report
test-cover-html: test-cover
	@echo "Generating HTML coverage report..."
	$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "Coverage report: $(COVERAGE_DIR)/coverage.html"

## bench: Run all benchmarks
bench:
	@echo "Running benchmarks..."
	$(GO) test -bench=. -benchtime=$(BENCH_TIME) -run=^$$ \
		./pkg/storage/... ./pkg/lsm/... ./pkg/query/... \
		./pkg/algorithms/... ./pkg/parallel/... ./pkg/wal/...

## bench-cpu: Run benchmarks with CPU profiling
bench-cpu:
	@echo "Running benchmarks with CPU profiling..."
	@mkdir -p $(COVERAGE_DIR)
	$(GO) test -bench=. -benchtime=$(BENCH_TIME) -run=^$$ \
		-cpuprofile=$(COVERAGE_DIR)/cpu.prof \
		./pkg/storage ./pkg/lsm ./pkg/query ./pkg/algorithms
	@echo "CPU profile: $(COVERAGE_DIR)/cpu.prof"
	@echo "To analyze: go tool pprof -http=:8080 $(COVERAGE_DIR)/cpu.prof"

## bench-mem: Run benchmarks with memory profiling
bench-mem:
	@echo "Running benchmarks with memory profiling..."
	@mkdir -p $(COVERAGE_DIR)
	$(GO) test -bench=. -benchtime=$(BENCH_TIME) -run=^$$ \
		-memprofile=$(COVERAGE_DIR)/mem.prof ./pkg/...
	@echo "Memory profile: $(COVERAGE_DIR)/mem.prof"
	@echo "To analyze: go tool pprof -http=:8080 $(COVERAGE_DIR)/mem.prof"

## build: Build main server binary
build:
	@echo "Building server..."
	@mkdir -p $(BINARY_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/server ./cmd/server

## build-all: Build all binaries
build-all:
	@echo "Building all binaries..."
	@mkdir -p $(BINARY_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/server ./cmd/server
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/cli ./cmd/cli
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/tui ./cmd/tui
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/tui-demo ./cmd/tui-demo
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/api-demo ./cmd/api-demo
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/import-dimacs ./cmd/import-dimacs
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/integration-test ./cmd/integration-test
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/graphdb-admin ./cmd/graphdb-admin
	@echo "All binaries built in $(BINARY_DIR)/"

## clean: Remove build artifacts and test data
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BINARY_DIR)
	rm -rf $(COVERAGE_DIR)
	rm -rf $(DATA_DIR)/test-*
	rm -rf $(DATA_DIR)/benchmark-*
	$(GO) clean -cache -testcache

## fmt: Format all Go code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "Running gofumpt (if available)..."
	@command -v gofumpt >/dev/null 2>&1 && gofumpt -l -w . || echo "gofumpt not installed, skipping"

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

## lint: Run golangci-lint (if available)
lint: vet
	@echo "Running golangci-lint..."
	@command -v golangci-lint >/dev/null 2>&1 && \
		golangci-lint run ./... || \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"

## run-server: Run the GraphDB server
run-server: build
	@echo "Starting GraphDB server..."
	$(BINARY_DIR)/server --port 8080 --data $(DATA_DIR)/server

## run-cli: Run the interactive CLI
run-cli: build-all
	@echo "Starting GraphDB CLI..."
	$(BINARY_DIR)/cli

## run-tui: Run the terminal UI
run-tui: build-all
	@echo "Starting GraphDB TUI..."
	$(BINARY_DIR)/tui

## install-tools: Install development tools
install-tools:
	@echo "Installing development tools..."
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install mvdan.cc/gofumpt@latest
	@echo "Tools installed!"

## mod-tidy: Tidy go.mod and go.sum
mod-tidy:
	@echo "Tidying go modules..."
	$(GO) mod tidy

## mod-verify: Verify go.mod dependencies
mod-verify:
	@echo "Verifying go modules..."
	$(GO) mod verify

## integration-test: Run integration tests (requires running server)
integration-test:
	@echo "Running integration tests..."
	@echo "Note: Ensure server is running on localhost:8080"
	@./test_api.sh || echo "Integration test script not executable or missing"

## api-test: Start server and run API tests
api-test: build
	@echo "Starting server and running API tests..."
	@$(BINARY_DIR)/server --port 8080 --data $(DATA_DIR)/api-test & \
		SERVER_PID=$$!; \
		sleep 2; \
		./test_api.sh; \
		TEST_EXIT=$$?; \
		kill $$SERVER_PID 2>/dev/null || true; \
		exit $$TEST_EXIT

## profile-cpu: Run CPU profiling on benchmarks
profile-cpu: bench-cpu
	@echo "Opening CPU profile in browser..."
	$(GO) tool pprof -http=:8080 $(COVERAGE_DIR)/cpu.prof

## profile-mem: Run memory profiling on benchmarks
profile-mem: bench-mem
	@echo "Opening memory profile in browser..."
	$(GO) tool pprof -http=:8080 $(COVERAGE_DIR)/mem.prof

## ci: Run all checks (for CI pipeline)
ci: mod-verify vet test-race test-cover
	@echo "✅ All CI checks passed!"

## dev: Quick development cycle (format, vet, test)
dev: fmt vet test-short
	@echo "✅ Development cycle complete!"

## all: Build everything and run tests
all: clean mod-tidy fmt vet test build-all
	@echo "✅ Full build complete!"
