# ADR-003: Event Sourcing in the Payment Service

## Status
Accepted

## Context
A payment's state (initiated, completed, failed, refunded, cancelled) has to be reconstructible
and auditable after the fact: which charge attempt produced which decision, in what order, is a
financial record, not just a current status field. Storing only the current row loses that
history on every update.

## Decision
The payment service persists a payment aggregate as an append-only stream of events in
`payment_events` (`aggregate_id`, `event_type`, `event_version`, `event_data`), rather than
updating a mutable row in place. `Repository.Load` rebuilds an aggregate by replaying its events
from the latest snapshot, if any, forward (`services/payment/internal/eventstore`);
`payment_snapshots` holds a full aggregate state every `SnapshotThreshold` versions (10) so
replay does not have to start from event 1 as the stream grows.

`Repository.Save` appends new events, updates the `payment_status` read model, and enqueues the
corresponding outbox message (see [ADR-002](./002-transactional-outbox.md)) all in one database
transaction, so the event log, the queryable projection and the outbound event are never out of
step with each other. `GET /api/v1/payments/{id}/events` exposes the raw event stream for audit;
`GET /api/v1/payments/{id}` and `GET /api/v1/payments` read the `payment_status` projection
instead of replaying events on every query.

## Consequences
### Positive
- The full history of a payment is available for audit and dispute resolution, not just its
  current state.
- Read queries stay cheap: they hit the `payment_status` projection, not a replay of the event
  stream.
- Snapshots bound replay cost for aggregates with a long event history.

### Negative
- Every write path has to go through the aggregate's command methods
  (`services/payment/internal/domain`) so state changes are always expressed as events; there is
  no shortcut for a direct row update.
- The projection and the event store must be kept in the same transaction by every writer, or they
  drift apart; there is no separate process reconciling them.
- Introduces two extra tables per aggregate (`payment_events`, `payment_snapshots`) beyond the
  projection, compared to a single mutable `payments` row.
