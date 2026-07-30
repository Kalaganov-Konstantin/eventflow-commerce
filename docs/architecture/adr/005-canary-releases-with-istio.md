# ADR-005: Canary Releases with Istio

## Status
Accepted

## Context
Rolling out a new version of a service straight to 100% of traffic means a bad build is fully
live before anyone notices. A staged rollout that shifts a small percentage of traffic first, and
rolls back automatically on an error spike, catches a bad build while it is only affecting a
fraction of requests.

## Decision
Every service gets a `stable` and a `canary` subset in its `DestinationRule`
(`infrastructure/istio/destination-rules.yaml`), selected by the `version` pod label, so any of
them is ready to run a canary rollout. Only the API Gateway currently has a second Deployment for
it (`infrastructure/k8s/base/api-gateway-canary.yaml`, `version: canary`, starting at zero
replicas) and a weighted route in its `VirtualService`
(`infrastructure/istio/virtual-services.yaml`): 100% to `stable`, 0% to `canary`, until a rollout
changes those weights.

`scripts/canary.sh` drives the rollout: `promote <service>` scales the canary Deployment to match
the stable Deployment's replica count, then steps the `VirtualService` weight through 5, 25, 50
and 100 percent, pausing `STABILIZE_SECONDS` (default 30s) after each step. Between steps it
queries Prometheus for the canary subset's 5xx rate over the last minute
(`istio_requests_total{destination_version="canary", response_code=~"5.."}`) and rolls back
automatically, scaling the canary Deployment back to zero and the weight back to 0%, if it exceeds
`ERROR_RATE_THRESHOLD` (default 5%). `rollback <service>` performs the same rollback on demand, and
`set <service> <weight>` patches an explicit weight for manual control. `make canary-promote
SERVICE=<service>` and `make canary-rollback SERVICE=<service>` wrap the two common cases.

## Consequences
### Positive
- A bad canary is caught and rolled back automatically from its 5xx rate, without an operator
  watching a dashboard through every step.
- The blast radius of a bad build is capped at the current step's traffic percentage instead of
  100% from the first request.
- Every service already has the `stable`/`canary` `DestinationRule` subsets in place, so wiring up
  a canary Deployment and `VirtualService` route for another service follows the same pattern the
  API Gateway already uses.

### Negative
- Only the API Gateway has a canary Deployment and a weighted route today; the other services
  route through a single `stable` destination and `scripts/canary.sh` has nothing to promote for
  them yet.
- The script depends on a reachable Prometheus at `$PROMETHEUS_URL`, and `infrastructure/k8s/base`
  does not ship a Prometheus Deployment; running a promotion means pointing the script at an
  existing Prometheus or port-forwarding one deployed separately.
- The canary image (tagged `:canary`) has to be built and pushed by hand before a promotion; there
  is no CI step that does this automatically.
