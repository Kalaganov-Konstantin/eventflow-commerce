# Performance tests

`order-flow.js` is a k6 scenario that drives the order creation endpoint through the api-gateway
of a running `make demo` stack. It mints its own JWTs with `k6/crypto`, so no separate auth step
is needed.

## Running

```bash
make test-performance
```

This brings up the full demo stack, seeds a product with enough stock to absorb the whole run,
and runs the scenario in the `grafana/k6` container against `http://host.docker.internal:<API_GATEWAY_PORT>`.

To run it directly against an already running stack:

```bash
docker run --rm --add-host=host.docker.internal:host-gateway \
  -e BASE_URL=http://host.docker.internal:8080 \
  -e JWT_SECRET=<same secret as the running api-gateway> \
  -e PRODUCT_ID=<id of a pre-seeded, in-stock product> \
  -v "$(pwd)/tests/performance:/scripts" \
  grafana/k6 run /scripts/order-flow.js
```

## Scenario

10 constant virtual users create an order every second for 30 seconds.

## Thresholds

- `http_req_duration`: p95 under 500ms.
- `order_errors`: error rate (any response other than 202) under 1%.

A threshold breach fails the k6 run with a non-zero exit code.

## Scope

This is not run in CI; it is meant for a local or staging environment with a full `make demo`
stack up. See `make test-integration` for the tests CI does run.
