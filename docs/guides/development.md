# Development Guide

This guide covers the day to day commands for working on EventFlow Commerce: bringing the stack
up, running tests at the right scope, and making sense of the two environment variable naming
schemes the services use.

Everything goes through the root `Makefile`; run `make help` for the full target list.

## Prerequisites

- Docker with the Compose plugin (or the standalone `docker-compose` binary)
- Go 1.25+
- Python 3.12 with [uv](https://docs.astral.sh/uv/)
- `jq`: the Go targets iterate the workspace with `go work edit -json | jq -r ".Use[].DiskPath"`,
  so a module without it is silently skipped

## Getting started

```bash
git clone https://github.com/Kalaganov-Konstantin/eventflow-commerce
cd eventflow-commerce
make setup   # go mod download, golangci-lint, goimports, uv sync for services/notification
make demo    # ensure-env + docker build + docker up --wait + migrate
```

`make demo` expands to four steps, each also usable on its own:

1. `ensure-env`: copies `.env.example` to `.env` if it does not already exist, and generates a real
   `JWT_SECRET` in place of the placeholder value.
2. `docker-build`: builds every service image.
3. `docker-up`: starts the stack with `docker compose up -d --wait`.
4. `migrate`: runs `migrate-order`, `migrate-payment`, `migrate-inventory` and
   `migrate-notification`, each a one-shot container behind the compose `tools` profile.

See the main `README.md` for the list of service and dashboard endpoints once the stack is up.

## Everyday commands

```bash
make docker-up      # start the stack without rebuilding images
make docker-down     # stop it
make docker-logs     # follow logs from every service
make docker-clean    # stop the stack and drop its volumes
make migrate         # (re)run migrations against the running stack
make logging-up      # start elasticsearch, kibana and fluentd (the "logging" compose profile)
make logging-down    # stop them
```

The logging stack is opt-in: `make demo` and a plain `docker-compose up` do not start
elasticsearch, kibana or fluentd, so a local run does not pay for three extra JVM-sized containers
by default.

## Testing

```bash
make test             # go test -race -cover per module, plus pytest for notification
make test-coverage     # same, aggregating into ./coverage.out and services/notification/coverage.xml
make test-integration  # go and python integration tests against real postgres, redis and kafka
make test-e2e          # end-to-end order flow test against a full make demo stack
make test-performance   # k6 order-flow scenario against a full make demo stack
make lint               # golangci-lint (every Go module) + ruff + black --check
make fmt                # gofmt -s, goimports, black, ruff --fix
```

CI runs `make lint`, `make test` and `make test-integration` on every push and pull request, plus a
separate pre-commit job and `govulncheck`. `make test-e2e` and `make test-performance` are not run
in CI: both expect a full `make demo` stack already up, which is heavier and slower than CI is
meant for. Run them locally, or against a staging environment, instead.

`make test-integration` starts its own dependency stack from `docker-compose.test.yml`: postgres on
port 5433, redis on 6380, kafka reachable on 9093 from the host. These ports are deliberately
different from the ones `docker-compose.yml` uses, so the integration stack can run alongside a
`make demo` stack without colliding with it. The target applies migrations directly with `psql` and
`yoyo` (not through the `migrate/migrate` containers `make migrate` uses), runs `go test
-tags=integration ./test/...` for order, payment and inventory, then `pytest -m integration` for
notification, and tears the dependency stack down through a `trap` regardless of the test result.

## Running a single test or package

Go modules do not share a workspace-level `go test ./...`; the repository root has no Go module of
its own, so run tests from inside the module:

```bash
cd services/api-gateway && go test -run TestAPIGateway_Integration ./test/
cd services/order && go test -race ./internal/handler/
```

Python:

```bash
cd services/notification && uv run pytest tests/test_main.py::test_health_check_endpoint
```

## Environment variable naming

Two naming schemes exist side by side; `.env.example` (and, in turn, `docker-compose.yml`) sets
both:

- The prefix each service derives from its own name through `shared/libs/go/config`:
  `config.New("order")` enables viper's `AutomaticEnv` with the prefix `ORDER`, so `server.port`
  resolves from `ORDER_SERVER_PORT`. This is the name `docker-compose.yml` actually passes into the
  container's environment (see `ORDER_SERVER_PORT=${ORDER_SERVICE_PORT}` in the `order-service`
  block), and the name `infrastructure/k8s/base/configmap.yaml` sets directly.
- The `*_SERVICE_PORT` names in `.env.example`, which exist only as the interpolation source Compose
  substitutes into the block above; nothing inside a container reads `ORDER_SERVICE_PORT` directly.

Each service's `LoadConfig` binds both names for a given setting through a variadic `BindEnv` (first
match wins), so the two schemes resolve to the same value either way:

```go
loader.BindEnv("server.port", "ORDER_SERVER_PORT", "ORDER_SERVICE_PORT")
```

A `Validate()` failure at startup names every bound variable, so check both forms when a service
refuses to start over a missing setting. The notification service has no second scheme: it reads
`NOTIFICATION_*` variables directly through pydantic-settings.

Tracing is the one variable that is not per-service: every service reads the same
`OTEL_EXPORTER_OTLP_ENDPOINT` (the Go services under an internal `jaeger.endpoint` config key), sent
as OTLP over HTTP rather than the old Jaeger thrift protocol; see
[ADR-004](../architecture/adr/004-otlp-instead-of-jaeger-thrift.md).

## Pre-commit hooks

Pre-commit runs `black` and `ruff` scoped to `services/notification/`, plus `make lint-go --fix` for
the Go workspace, along with generic whitespace, YAML, JSON and merge-conflict checks. CI runs the
same hooks as its own job.

1. Install pre-commit:

   ```bash
   pip install pre-commit
   ```

2. Install the git hooks:

   ```bash
   pre-commit install
   ```

The hooks then run automatically on `git commit`. Run them manually at any time with:

```bash
pre-commit run --all-files
```
