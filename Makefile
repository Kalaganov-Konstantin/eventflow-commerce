.PHONY: help
help: ## ✨ Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# =============================================================================
# PROJECT COMMANDS
# =============================================================================

.PHONY: setup
setup: ## 📦 Install all project dependencies
	@echo "--> Installing Go dependencies..."
	@go mod download
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "--> Installing Python dependencies for the 'notification' service..."
	@cd services/notification && uv sync --locked --all-extras --dev

.PHONY: lint
lint: lint-go-ci lint-python ## 🔍 Run all linters (CI version)
	@echo "✅ All linters passed successfully."

.PHONY: test
test: test-go test-python ## 🧪 Run all unit tests
	@echo "✅ All tests passed successfully."


# =============================================================================
# INTERNAL TARGETS (for local development and CI)
# =============================================================================

# --- Linting ---
.PHONY: lint-go
lint-go: ## (internal) Run Go linters with auto-fix (for pre-commit)
	@echo "--> Linting Go modules (local mode with --fix)..."
	@for mod in $$(go work edit -json | jq -r ".Use[].DiskPath"); do \
		if [ -n "$$(find "$$mod" -name "*.go" -print -quit)" ]; then \
			echo "Linting $$mod..."; \
			(cd "$$mod" && golangci-lint run --fix); \
		else \
			echo "Skipping $$mod (no .go files found)."; \
		fi; \
	done

.PHONY: lint-go-ci
lint-go-ci: ## (internal) Run Go linters without auto-fix (for CI)
	@echo "--> Linting Go modules (CI mode)..."
	@for mod in $$(go work edit -json | jq -r ".Use[].DiskPath"); do \
		if [ -n "$$(find "$$mod" -name "*.go" -print -quit)" ]; then \
			echo "Linting $$mod..."; \
			(cd "$$mod" && golangci-lint run --timeout=5m); \
		else \
			echo "Skipping $$mod (no .go files found)."; \
		fi; \
	done

.PHONY: lint-python
lint-python: ## (internal) Run Python linters
	@echo "--> Linting 'notification' service (ruff and black)..."
	@cd services/notification && uv run ruff check .
	@cd services/notification && uv run black --check .

# --- Testing ---
.PHONY: test-go
test-go: ## (internal) Run Go unit tests for all modules
	@echo "--> Running Go unit tests..."
	@for mod in $$(go work edit -json | jq -r ".Use[].DiskPath"); do \
		if [ -n "$$(find "$$mod" -name "*_test.go" -print -quit)" ]; then \
			echo "Testing $$mod..."; \
			(cd "$$mod" && go test -v -race -cover ./...); \
		else \
			echo "Skipping $$mod (no tests found)."; \
		fi; \
	done

.PHONY: test-python
test-python: ## (internal) Run Python unit tests
	@echo "--> Running Python unit tests for 'notification' service..."
	@cd services/notification && uv run pytest
