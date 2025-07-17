.PHONY: help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# =============================================================================
# Python Virtual Environment Check
# =============================================================================

# This is a guard to ensure Python commands are run inside a virtual environment.
# It checks for the VIRTUAL_ENV environment variable, which is set when a venv is active.
check-venv:
	@if [ -z "$$VIRTUAL_ENV" ]; then \
		echo "ERROR: Python virtual environment is not activated."; \
		echo "Please create and activate it first. Example:"; \
		echo "  python3 -m venv .venv"; \
		echo "  source .venv/bin/activate"; \
		exit 1; \
	fi

# =============================================================================
# Development Commands
# =============================================================================

.PHONY: setup
setup: check-venv ## 📦 Install all dependencies
	@echo "Installing Go dependencies..."
	@go mod download
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Installing Python dependencies into active venv..."
	@cd services/notification && pip install -r requirements.txt

.PHONY: lint
lint: check-venv ## 🔍 Run all linters
	@echo "Running Go linters..."
	@golangci-lint run --timeout=5m ./...
	@echo "Running Python linters..."
	@cd services/notification && flake8 . && black --check .

.PHONY: test
test: ## 🧪 Run unit tests
	@echo "Running Go unit tests..."
	@go test -v -race -cover ./...
	@# Add python test command here when ready
