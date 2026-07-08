-include .env

# GitHub-hosted runners ship the `docker compose` plugin but not the standalone `docker-compose`
# binary; Docker Desktop's shim provides both locally, which is why this went unnoticed until CI.
COMPOSE := $(shell command -v docker-compose >/dev/null 2>&1 && echo docker-compose || echo docker compose)

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
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2
	@go install golang.org/x/tools/cmd/goimports@latest
	@echo "--> Installing Python dependencies for the 'notification' service..."
	@cd services/notification && uv sync --locked --all-extras --dev

.PHONY: lint
lint: lint-go-ci lint-python ## 🔍 Run all linters (CI version)
	@echo "✅ All linters passed successfully."

.PHONY: fmt
fmt: fmt-go fmt-python ## 🎨 Format all code (Go + Python)
	@echo "✅ All code formatted successfully."

.PHONY: test
test: test-go test-python ## 🧪 Run all unit tests
	@echo "✅ All tests passed successfully."

.PHONY: test-coverage
test-coverage: test-go-coverage test-python-coverage ## 📊 Run all tests with coverage
	@echo "✅ All tests with coverage completed successfully."

.PHONY: migrate
migrate: migrate-order migrate-payment migrate-inventory migrate-notification ## 🚀 Run all database migrations
	@echo "✅ All migrations applied successfully."


# =============================================================================
# INTERNAL TARGETS (for local development and CI)
# =============================================================================

# --- Migrations ---
.PHONY: migrate-order
migrate-order: ## (internal) Run database migrations for the order service
	@echo "--> Running migrations for 'order' service..."
	@$(COMPOSE) run --rm order-migrate

.PHONY: migrate-payment
migrate-payment: ## (internal) Run database migrations for the payment service
	@echo "--> Running migrations for 'payment' service..."
	@$(COMPOSE) run --rm payment-migrate

.PHONY: migrate-inventory
migrate-inventory: ## (internal) Run database migrations for the inventory service
	@echo "--> Running migrations for 'inventory' service..."
	@$(COMPOSE) run --rm inventory-migrate

.PHONY: migrate-notification
migrate-notification: ## (internal) Run database migrations for the notification service
	@echo "--> Running migrations for 'notification' service..."
	@$(COMPOSE) run --rm notification-migrate

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
			(cd "$$mod" && golangci-lint run --timeout=5m) || exit 1; \
		else \
			echo "Skipping $$mod (no .go files found)."; \
		fi; \
	done

.PHONY: lint-python
lint-python: ## (internal) Run Python linters
	@echo "--> Linting 'notification' service (ruff and black)..."
	@cd services/notification && uv run ruff check .
	@cd services/notification && uv run black --check .

# --- Formatting ---
.PHONY: fmt-go
fmt-go: ## (internal) Format Go code with gofmt and goimports
	@echo "--> Formatting Go code..."
	@for mod in $$(go work edit -json | jq -r ".Use[].DiskPath"); do \
		if [ -n "$$(find "$$mod" -name "*.go" -print -quit)" ]; then \
			echo "Formatting $$mod..."; \
			find "$$mod" -name "*.go" -exec gofmt -s -w {} \;; \
			find "$$mod" -name "*.go" -exec goimports -w {} \;; \
		else \
			echo "Skipping $$mod (no .go files found)."; \
		fi; \
	done

.PHONY: fmt-python
fmt-python: ## (internal) Format Python code with black and ruff
	@echo "--> Formatting 'notification' service (black and ruff)..."
	@cd services/notification && uv run black .
	@cd services/notification && uv run ruff check --fix .

# --- Testing ---
.PHONY: test-go
test-go: ## (internal) Run Go unit tests for all modules
	@echo "--> Running Go unit tests..."
	@for mod in $$(go work edit -json | jq -r ".Use[].DiskPath"); do \
		if [ -n "$$(find "$$mod" -name "*_test.go" -print -quit)" ]; then \
			echo "Testing $$mod..."; \
			(cd "$$mod" && go test -v -race -cover ./...) || exit 1; \
		else \
			echo "Skipping $$mod (no tests found)."; \
		fi; \
	done

.PHONY: test-python
test-python: ## (internal) Run Python unit tests
	@echo "--> Running Python unit tests for 'notification' service..."
	@cd services/notification && uv run pytest

# --- Coverage Testing ---
.PHONY: test-go-coverage
test-go-coverage: ## (internal) Run Go unit tests with coverage
	@echo "--> Running Go unit tests with coverage..."
	@echo "mode: atomic" > coverage.out
	@ROOT_DIR=$$(pwd); \
	for mod in $$(go work edit -json | jq -r ".Use[].DiskPath"); do \
		if [ -n "$$(find "$$mod" -name "*_test.go" -print -quit)" ]; then \
			echo "Testing $$mod with coverage..."; \
			(cd "$$mod" && go test -v -race -coverprofile=coverage.tmp -covermode=atomic ./... && \
			if [ -f coverage.tmp ]; then \
				tail -n +2 coverage.tmp >> "$$ROOT_DIR/coverage.out" && rm coverage.tmp; \
			fi) || exit 1; \
		else \
			echo "Skipping $$mod (no tests found)."; \
		fi; \
	done
	@if [ ! -s coverage.out ]; then \
		echo "Warning: Go coverage file is empty or missing"; \
	else \
		echo "✅ Go coverage file generated with $$(wc -l < coverage.out) lines"; \
	fi

.PHONY: test-python-coverage
test-python-coverage: ## (internal) Run Python unit tests with coverage
	@echo "--> Running Python unit tests with coverage for 'notification' service..."
	@cd services/notification && uv run pytest --cov=notification --cov-report=xml:coverage.xml --cov-report=term-missing

# =============================================================================
# INTEGRATION TEST DEPENDENCIES
# =============================================================================

.PHONY: test-deps-up
test-deps-up: ## (internal) Start postgres, redis and kafka for integration tests
	@echo "--> Starting integration test dependencies..."
	@$(COMPOSE) -f docker-compose.test.yml up -d --wait

.PHONY: test-deps-down
test-deps-down: ## (internal) Stop the integration test dependencies
	@echo "--> Stopping integration test dependencies..."
	@$(COMPOSE) -f docker-compose.test.yml down -v

.PHONY: test-migrate
test-migrate: ## (internal) Apply migrations to the integration test databases
	@echo "--> Applying migrations to the integration test databases..."
	@for f in services/order/migrations/*.up.sql; do \
		PGPASSWORD=orders_pass psql -h localhost -p 5433 -U orders_user -d orders -v ON_ERROR_STOP=1 -q -f "$$f" || exit 1; \
	done
	@for f in services/payment/migrations/*.up.sql; do \
		PGPASSWORD=payments_pass psql -h localhost -p 5433 -U payments_user -d payments -v ON_ERROR_STOP=1 -q -f "$$f" || exit 1; \
	done
	@for f in services/inventory/migrations/*.up.sql; do \
		PGPASSWORD=inventory_pass psql -h localhost -p 5433 -U inventory_user -d inventory -v ON_ERROR_STOP=1 -q -f "$$f" || exit 1; \
	done
	@cd services/notification && uv run yoyo apply --batch \
		--database "postgresql://notifications_user:notifications_pass@localhost:5433/notifications" ./migrations

.PHONY: test-go-integration
test-go-integration: ## (internal) Run Go integration tests (tag integration) against test-deps
	@echo "--> Running Go integration tests..."
	@for mod in services/order services/payment services/inventory; do \
		echo "Integration testing $$mod..."; \
		(cd "$$mod" && go test -tags=integration -race ./test/...) || exit 1; \
	done

.PHONY: test-python-integration
test-python-integration: ## (internal) Run Python integration tests (marker integration) against test-deps
	@echo "--> Running Python integration tests..."
	@cd services/notification && uv run pytest -m integration

.PHONY: test-integration
test-integration: ## 🧪 Run integration tests against real postgres, redis and kafka
	@trap '$(MAKE) test-deps-down' EXIT; \
	$(MAKE) test-deps-up && \
	$(MAKE) test-migrate && \
	$(MAKE) test-go-integration && \
	$(MAKE) test-python-integration

# =============================================================================
# END TO END AND PERFORMANCE TESTS
# =============================================================================

.PHONY: test-e2e
test-e2e: ## 🧪 Run the end to end order flow test against a full demo stack
	@$(MAKE) demo
	@cd tests/e2e && JWT_SECRET=$(JWT_SECRET) go test -tags=e2e -race ./...

.PHONY: test-performance
test-performance: ## 🚀 Run the k6 order flow scenario against a full demo stack
	@$(MAKE) demo
	@echo "--> Seeding a product for the performance scenario..."
	@PRODUCT_ID=$$(uuidgen | tr 'A-Z' 'a-z'); \
	$(COMPOSE) exec -T postgres psql -U inventory_user -d inventory -v ON_ERROR_STOP=1 -q -c \
		"INSERT INTO products (id, name, sku, price_cents, currency, is_active, created_at, updated_at, version) VALUES ('$$PRODUCT_ID', 'Load Test Widget', 'PERF-'||substr('$$PRODUCT_ID', 1, 8), 999, 'USD', true, NOW(), NOW(), 1)"; \
	$(COMPOSE) exec -T postgres psql -U inventory_user -d inventory -v ON_ERROR_STOP=1 -q -c \
		"INSERT INTO inventory (product_id, quantity_available, quantity_reserved) VALUES ('$$PRODUCT_ID', 1000000, 0)"; \
	docker run --rm --add-host=host.docker.internal:host-gateway \
		-e BASE_URL=http://host.docker.internal:$(API_GATEWAY_PORT) \
		-e JWT_SECRET=$(JWT_SECRET) \
		-e PRODUCT_ID=$$PRODUCT_ID \
		-v $(CURDIR)/tests/performance:/scripts \
		grafana/k6 run /scripts/order-flow.js

# =============================================================================
# DOCKER COMMANDS
# =============================================================================

.PHONY: docker-build
docker-build: ## 🐳 Build all Docker images
	@echo "--> Building all Docker images..."
	@$(COMPOSE) build

.PHONY: docker-up
docker-up: ## 🚀 Start all services with Docker Compose
	@echo "--> Starting all services and waiting for them to be healthy..."
	@$(COMPOSE) up -d --wait

.PHONY: docker-down
docker-down: ## 🛑 Stop all services
	@echo "--> Stopping all services..."
	@$(COMPOSE) down

.PHONY: docker-logs
docker-logs: ## 📋 Show logs from all services
	@$(COMPOSE) logs -f

.PHONY: docker-clean
docker-clean: ## 🧹 Clean Docker images and containers
	@echo "--> Cleaning Docker resources..."
	@$(COMPOSE) down -v
	@docker system prune -f
	@docker volume prune -f

.PHONY: logging-up
logging-up: ## 📚 Start the logging stack (elasticsearch, kibana, fluentd)
	@echo "--> Starting the logging stack and waiting for it to be healthy..."
	@$(COMPOSE) --profile logging up -d --wait elasticsearch kibana fluentd
	@echo "✅ Logging stack is running!"
	@echo "📚 Kibana: http://localhost:${KIBANA_PORT}"

.PHONY: logging-down
logging-down: ## 🛑 Stop the logging stack
	@echo "--> Stopping the logging stack..."
	@$(COMPOSE) --profile logging down elasticsearch kibana fluentd


.PHONY: demo
demo: ensure-env docker-build docker-up migrate ## 🎯 Full demo: build and start all services
	@echo "✅ EventFlow Commerce is running!"
	@echo "🌐 API Gateway: http://localhost:${API_GATEWAY_PORT}"
	@echo "📊 Grafana: http://localhost:${GRAFANA_PORT} (admin/admin)"
	@echo "🔍 Jaeger: http://localhost:${JAEGER_UI_PORT}"
	@echo "📈 Prometheus: http://localhost:${PROMETHEUS_PORT}"
	@echo "⚙️ Kafka UI: http://localhost:${KAFKA_UI_PORT}"

SERVICE ?= api-gateway

.PHONY: canary-promote
canary-promote: ## 🐤 Shift SERVICE's canary through the 5/25/50/100 percent traffic steps
	@bash scripts/canary.sh promote $(SERVICE)

.PHONY: canary-rollback
canary-rollback: ## ⏪ Roll SERVICE's canary back to zero traffic
	@bash scripts/canary.sh rollback $(SERVICE)

.PHONY: ensure-env
ensure-env: ## 🔐 Create .env file from template if it doesn't exist, with secure JWT secret
	@if [ ! -f .env ]; then \
		echo "--> Creating .env file from .env.example..."; \
		cp .env.example .env; \
		JWT_SECRET=$$(openssl rand -base64 32); \
		ESCAPED_SECRET=$$(echo "$$JWT_SECRET" | sed 's/[\/&]/\\&/g'); \
		if [ "$$(uname)" = "Darwin" ]; then \
			sed -i '' "s/JWT_SECRET=CHANGE_ME_IN_PRODUCTION_GENERATE_WITH_openssl_rand_base64_32/JWT_SECRET=$$ESCAPED_SECRET/" .env; \
		else \
			sed -i "s/JWT_SECRET=CHANGE_ME_IN_PRODUCTION_GENERATE_WITH_openssl_rand_base64_32/JWT_SECRET=$$ESCAPED_SECRET/" .env; \
		fi; \
		echo "✅ .env file created with secure JWT secret"; \
	else \
		echo "✅ .env file already exists"; \
	fi
