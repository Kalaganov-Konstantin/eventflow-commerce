# ADR-002: Transactional Outbox for Domain Events

## Status
Accepted

## Context
Order, payment and inventory each commit a state change to Postgres and then need to announce
it on Kafka: order.created, payment.processed, inventory.reserved, and so on. Writing to the
database and publishing to Kafka are two separate systems with no shared transaction. A
publish-after-commit call that fails leaves the state change without its event; a publish before
commit can announce a change that then rolls back. Either way, downstream consumers such as the
order saga and the notification service end up out of sync with what actually happened.

## Decision
Every domain event is written as a row in that service's own `outbox_messages` table, in the
same database transaction as the state change it describes (`shared/libs/go/outbox.Store`,
`Enqueue`). A separate relay goroutine (`shared/libs/go/outbox.Relay`) polls the table on an
interval, publishes pending rows to Kafka through the shared publisher, and marks them published,
selecting with `FOR UPDATE SKIP LOCKED` so more than one relay instance can run without
publishing the same row twice.

On the consuming side, each service records handled event IDs in a `processed_events` table
(`shared/libs/go/events.ProcessedStore`) before acting on an event, so a redelivered message
(from a consumer crash before its offset commits, or from the relay retrying a row it already
published) is recognized and skipped instead of reapplied.

## Consequences
### Positive
- The state change and the event that announces it either both commit or neither does; there is
  no window where one happens without the other.
- Consumers tolerate at-least-once delivery: redelivery after a crash is a no-op instead of a
  duplicate side effect.
- The relay is a plain SQL poller with no dependency on Kafka transactions or exactly-once
  semantics from the broker.

### Negative
- An event becomes visible on Kafka only after the next relay poll, not synchronously with the
  commit; each service's `outbox.relay_interval` (for example 1s for order, see
  `services/order/internal/config`) is a lower bound on that delay, not a guarantee.
- Every service that publishes events carries its own `outbox_messages` table, relay goroutine and
  polling loop; this is duplicated infrastructure rather than a single shared component, because
  each service owns its own database.
- A poll-based relay adds load proportional to the poll interval regardless of whether there is
  anything to publish, unlike a push-based CDC approach.
