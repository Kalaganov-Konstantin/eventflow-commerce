# Caching Strategy

This document describes the caching strategies used in EventFlow Commerce to improve performance and reduce database load.

### Data Flow Through Cache

**Read Path (Cache-Aside)**

```mermaid
flowchart LR
    style Cache fill:#f96,stroke:#333,stroke-width:2px

    Client --> GW(API Gateway)
    GW --> S["Service<br>(Cache-Aside Logic)"]
    S <--> DB[(Database)]
    S <--> Cache[Redis Cache]
```

**Write Path (Async Invalidation)**

```mermaid
flowchart LR
    style Cache fill:#f96,stroke:#333,stroke-width:2px

    Client --> GW(API Gateway)
    GW --> S[Service]
    S --> DB[(Database)]
    S --> K[Kafka]
    K --> Consumer[Cache Invalidation<br/>Consumer]
    Consumer --> Cache[Redis Cache]
```
