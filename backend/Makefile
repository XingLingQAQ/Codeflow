.PHONY: all build test lint clean help build-all build-frontend

# Variables
BINARY_NAME=codeflow-server
BUILD_DIR=build
MAIN_PATH=./cmd/codeflow-server
GO=go
GOFLAGS=-v
COVERAGE_OUT?=coverage.out
COVERAGE_HTML?=coverage.html
COVERAGE_PKGS?=./...
LDFLAGS=-ldflags "-s -w"
FRONTEND_DIR=../apps/workbench
WEB_DIST_DIR=./internal/web/dist

# Default target
all: lint test build

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -race -coverprofile=$(COVERAGE_OUT) $(COVERAGE_PKGS)
	@echo "Tests complete"

# Run tests with coverage report
test-coverage: test
	$(GO) tool cover -html=$(COVERAGE_OUT) -o $(COVERAGE_HTML)
	$(GO) tool cover -func=$(COVERAGE_OUT) > coverage.txt
	@echo "Coverage report: $(COVERAGE_HTML)"
	@echo "Coverage summary: coverage.txt"

# Run linter
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
	golangci-lint run ./...; \
	else \
	echo "golangci-lint not installed. Run: make install-lint"; \
	fi

# Install golangci-lint
install-lint:
	@echo "Installing golangci-lint..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "Format complete"

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GO) mod tidy
	@echo "Tidy complete"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f $(COVERAGE_OUT) $(COVERAGE_HTML) coverage.txt
	@echo "Clean complete"

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME)

# Development mode (build and run)
dev:
	@echo "Development mode..."
	$(GO) run $(MAIN_PATH)

# Show help
help:
	@echo "CodeFlow Backend Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all            - Run lint, test, and build (default)"
	@echo "  build          - Build the application"
	@echo "  build-all      - Build frontend + backend (single exe)"
	@echo "  build-frontend - Build frontend and sync embed dist"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  lint           - Run golangci-lint"
	@echo "  install-lint   - Install golangci-lint"
	@echo "  fmt            - Format code"
	@echo "  tidy           - Tidy dependencies"
	@echo "  clean          - Clean build artifacts"
	@echo "  run            - Build and run the application"
	@echo "  dev            - Run in development mode"
	@echo "  help           - Show this help message"

# Build frontend (prefer monorepo pnpm from repo root) and sync to embed path
build-frontend:
	@echo "Building frontend..."
	@if [ -f ../pnpm-workspace.yaml ]; then \
		cd .. && \
		if [ ! -d node_modules ]; then pnpm install; fi && \
		pnpm --filter @codeflow/workbench build; \
	else \
		cd $(FRONTEND_DIR) && npm install && npm run build; \
	fi
	@echo "Syncing frontend dist to $(WEB_DIST_DIR)..."
	@if [ ! -d $(FRONTEND_DIR)/dist ]; then \
		echo "Error: Frontend dist not found: $(FRONTEND_DIR)/dist"; \
		exit 1; \
	fi
	@rm -rf $(WEB_DIST_DIR)
	@mkdir -p $(WEB_DIST_DIR)
	@cp -R $(FRONTEND_DIR)/dist/. $(WEB_DIST_DIR)/
	@test -f $(WEB_DIST_DIR)/index.html
	@echo "Frontend build complete: $(WEB_DIST_DIR)"

# Build all (frontend + backend with embedded assets)
# CGO required: mattn/go-sqlite3 (see docs/adr/0003-sqlite-cgo.md)
build-all: build-frontend
	@echo "Building $(BINARY_NAME) with embedded frontend..."
	@mkdir -p $(BUILD_DIR)
	@if [ ! -d $(WEB_DIST_DIR) ] || [ ! -f $(WEB_DIST_DIR)/index.html ]; then \
		echo "Error: Embed dist missing at $(WEB_DIST_DIR). Run make build-frontend first."; \
		exit 1; \
	fi
	CGO_ENABLED=1 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME).exe $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME).exe"
	@echo "Run with: $(BUILD_DIR)/$(BINARY_NAME).exe"
	@echo "Access at: http://localhost:8080"