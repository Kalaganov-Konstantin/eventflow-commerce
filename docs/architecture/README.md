# Architecture Documentation

This directory contains detailed documentation on the architectural patterns and decisions that power EventFlow Commerce. 

For a high-level overview, please see the main [README.md](../README.md).

## 🏛️ Architectural Patterns

- **[Deployment Architecture](./deployment-architecture.md)**: An overview of how the system is deployed on Kubernetes with Istio.
- **[Saga Pattern](./saga-pattern.md)**: How we handle distributed transactions to ensure data consistency.
- **[Event Sourcing](./event-sourcing.md)**: The pattern used in our Payment Service for full auditability.
- **[Resilience Patterns](./resilience.md)**: How we build a fault-tolerant system using Circuit Breakers.
- **[Caching Strategy](./caching.md)**: Our approach to caching for improved performance.
- **[Observability](./observability.md)**: Our stack for monitoring, logging, and tracing.
- **[Distributed Tracing](./distributed-tracing.md)**: An example of a request trace through the system.
- **[Zero-Downtime Deployment](./zero-downtime-deployment.md)**: Our Canary Release strategy for safe deployments.

## 📜 Architecture Decision Records (ADRs)

For a log of all significant architectural decisions, see the **[Architecture Decision Records (ADRs)](./adr/README.md)** directory.