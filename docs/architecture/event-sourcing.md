# Event Sourcing

How the payment service stores a payment as an append-only stream of events instead of a mutable
row, and how it keeps that stream, a queryable projection and the outbound Kafka event all
consistent with each other. See [ADR-003](./adr/003-event-sourcing-in-payments.md) for why.

Implemented in `services/payment/internal/domain` (the aggregate and its events),
`services/payment/internal/eventstore` (the event store, snapshots and the repository that ties
them together with the projection and the outbox), and `services/payment/internal/projection`
(the `payment_status` read model).

### Conceptual overview

```mermaid
graph LR
    subgraph "Commands"
        CMD1[Initiate + Process/Fail<br/>ProcessPayment]
        CMD2[Refund<br/>RefundPayment]
    end

    subgraph "Payment Aggregate"
        PA["Payment<br>ID, OrderID, CustomerID<br>AmountCents, Currency<br>Status, Version"]
    end

    subgraph "Events (payment_events.event_type)"
        E1[payment.initiated]
        E2[payment.processed]
        E3[payment.failed]
        E4[payment.refunded]
        E5[payment.cancelled]
    end

    subgraph "Event Store (PostgreSQL)"
        ES[(payment_events<br/>+ payment_snapshots every 10 versions)]
    end

    subgraph "Read Model"
        RM[payment_status projection]
    end

    CMD1 --> PA
    CMD2 --> PA

    PA --> E1
    PA --> E2
    PA --> E3
    PA --> E4
    PA --> E5

    E1 --> ES
    E2 --> ES
    E3 --> ES
    E4 --> ES
    E5 --> ES

    ES --> RM

    style ES fill:#f96,stroke:#333,stroke-width:2px
```

`domain.Payment.Cancel` and the `payment.cancelled` event exist in the domain model but nothing
in the current codebase calls `Cancel`; only initiate/process/fail and refund are reachable
through the HTTP API.

### Write path

A single call to `ProcessPayment` produces two pending events on the aggregate, not one:
`Initiate` always appends `payment.initiated`, and the gateway result then appends either
`payment.processed` or `payment.failed`. `Repository.Save` persists all of an aggregate's pending
events in one database transaction:

```mermaid
graph LR
    subgraph "Write Path (Repository.Save, one transaction)"
        direction LR
        CMD["Command
(ProcessPayment / RefundPayment)"] --> AGG["Payment aggregate
(Apply appends pending events,
bumps Version)"]
        AGG -- "1. Append events" --> ES[("payment_events
+ snapshot every
SnapshotThreshold=10")]
        AGG -- "2. Update projection" --> RM[("payment_status")]
        AGG -- "3. Enqueue outbox row
per event" --> OUTBOX[("outbox_messages")]
    end

    subgraph "Relay (separate process, polling)"
        OUTBOX --> RELAY["Outbox relay"]
        RELAY --> KAFKA["payments.events"]
    end

    ES -. "Load: snapshot + events since" .-> AGG
```

`Repository.Load` rebuilds an aggregate for a command by reading its latest snapshot (if any) and
replaying every event recorded since. `GET /api/v1/payments/{id}/events` exposes the raw stream
for audit; `GET /api/v1/payments/{id}` and `GET /api/v1/payments` read `payment_status` instead of
replaying events on every request, since replay cost only matters on the write path where the
aggregate must be reloaded to accept the next command.
