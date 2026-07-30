# API Documentation

OpenAPI 3.0 specifications for the four HTTP APIs in EventFlow Commerce, generated from the
actual handlers rather than the target design in `docs/architecture/`.

*   [orders.yaml](./orders.yaml): the order service (create, get, list orders).
*   [payments.yaml](./payments.yaml): the payment service (charge, refund, query, event history).
*   [inventory.yaml](./inventory.yaml): the inventory service (product catalog, stock reservations).
*   [notifications.yaml](./notifications.yaml): the notification service (read-only notification lookup).

Lint all four with [Redocly CLI](https://redocly.com/docs/cli/):

```bash
docker run --rm -v "$PWD/docs/api:/spec" redocly/cli lint /spec/orders.yaml /spec/payments.yaml /spec/inventory.yaml /spec/notifications.yaml
```

## Talking to a service

Every path below is served both directly by its service and, unchanged, through the API
Gateway at `http://localhost:8080` (the gateway proxies without stripping the `/api/v1/...`
prefix). A client going through the gateway authenticates with a JWT bearer token; the gateway
validates it and forwards the claims as `X-User-ID`, `X-User-Email` and `X-User-Role` headers,
which is the header the order, payment and notification specs document as a request parameter.
A client calling a service directly bypasses that check and must set `X-User-ID` itself.

## Conventions across all four specs

*   **Money** is always an integer number of minor currency units (cents), in fields named
    `*_cents`, never a float.
*   **Errors** from the three Go services share one shape, `AppError` (`shared/libs/go/errors`):
    `{"code": "...", "message": "...", "details": "..."}`, with `details` omitted when empty. The
    notification service is a FastAPI app and still uses FastAPI's default
    `{"detail": "..."}` shape; see `notifications.yaml`.
*   IDs are UUIDs everywhere.

## Known gaps

*   The order, payment and inventory services expose `/health`, `/health/live`, `/health/ready`
    and `/metrics` in addition to their `/api/v1/...` routes; these are operational endpoints,
    not part of the business API, and are not spec'd here.
*   The payment gateway and SMS delivery behind these APIs are stubs; see the main
    [README](../../README.md).
