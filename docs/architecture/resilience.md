# Resilience Patterns

This document describes the resilience patterns used in EventFlow Commerce, focusing on the Circuit Breaker pattern.

### Error Handling & Circuit Breaker

**State Machine**

```mermaid
graph TB
    subgraph "Circuit Breaker States"
        CLOSED[Closed<br/>All requests pass]
        OPEN[Open<br/>All requests fail fast]
        HALF[Half-Open<br/>Limited requests]
    end
    
    subgraph "Metrics"
        SUCC[Success Count]
        FAIL[Failure Count]
        TIMEOUT[Timeout Count]
    end
    
    CLOSED -->|Failure Threshold| OPEN
    OPEN -->|After Timeout| HALF
    HALF -->|Success| CLOSED
    HALF -->|Failure| OPEN
    
    style CLOSED fill:#9f9,stroke:#333,stroke-width:2px
    style OPEN fill:#f99,stroke:#333,stroke-width:2px
    style HALF fill:#ff9,stroke:#333,stroke-width:2px
```

**Execution Flow with Fallback**

```mermaid
flowchart LR
    A(Request) --> B{Breaker Open?}
    B -- "No" --> C[Execute Remote Call]
    C --> D{Success?}
    D -- "Yes" --> E(Return Result)
    D -- "No" --> F[Execute Fallback]
    B -- "Yes" --> F
    F --> G(Return Fallback Result or Error)
```