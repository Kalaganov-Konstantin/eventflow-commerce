# Deployment Guide

This guide deploys EventFlow Commerce to Kubernetes with Istio as the service mesh, then walks
through a canary release. The manifests live in `infrastructure/k8s/` (kustomize) and
`infrastructure/istio/` (plain Istio custom resources, applied separately).

## Prerequisites

- A Kubernetes cluster (kind or minikube for a local try, anything else for real use) with
  `kubectl` configured against it
- Istio installed in the cluster; this repository only ships the mesh's namespace-level
  configuration (Gateway, VirtualServices, DestinationRules, PeerAuthentication, AuthorizationPolicies,
  Telemetry), not the Istio control plane itself
- An `otel` tracing extension provider registered in the cluster's Istio install
  (`meshConfig.extensionProviders`), pointing at the same OTLP collector the services already
  export to. This is cluster-level Istio configuration, not part of this repository; without it the
  `Telemetry` resource in `infrastructure/istio/telemetry.yaml` has nothing to attach to
- `kustomize` (bundled with recent `kubectl` as `kubectl apply -k`)

## 1. Namespace and secrets

```bash
kubectl apply -f infrastructure/k8s/base/namespace.yaml
```

Every Deployment in the base reads database URLs, the JWT signing key and SMTP credentials from a
`Secret` named `eventflow-secrets`, injected through `envFrom.secretRef`. That secret is
deliberately not one of `kustomization.yaml`'s resources, so `kubectl apply -k` never creates it and
the real values never get committed:

```bash
cp infrastructure/k8s/base/secrets.example.yaml /tmp/eventflow-secrets.yaml
# edit /tmp/eventflow-secrets.yaml: postgres credentials, each *_DATABASE_URL, JWT_SECRET
# (32+ characters, must not be a known weak/placeholder value), optional SMTP credentials
kubectl apply -f /tmp/eventflow-secrets.yaml
```

## 2. Application manifests

```bash
kubectl apply -k infrastructure/k8s/overlays/dev
```

Use `overlays/prod` instead for a cluster with real capacity: it patches the base up to three
replicas for the API Gateway and order service (the two hops every request takes), adds node
anti-affinity for the rest, and applies production-sized resource requests and a `production`
logger environment. `overlays/dev` instead pins every Deployment to one replica and shrinks resource
requests, since a local cluster has no spare capacity for the base's rolling-update surge or its
HPA minimums.

The base bundles the migration `Jobs` (`infrastructure/k8s/base/migrations.yaml`) together with the
application `Deployments`, and Kubernetes has no ordering primitive between them. On a cluster
that is starting from empty volumes, expect the application pods to crash loop for a short while
until each migration `Job` completes and the schema exists; they recover on their own once it does.
Confirm the migrations actually finished rather than reading too much into early restarts:

```bash
kubectl wait --for=condition=complete job -l eventflow.io/phase=migrate -n eventflow --timeout=300s
```

If a migration `Job` fails outright (for example because postgres was not yet reachable within its
`backoffLimit`), delete and reapply it: `kubectl delete job <name>-migrate -n eventflow && kubectl
apply -f infrastructure/k8s/base/migrations.yaml`.

## 3. Istio configuration

```bash
kubectl label namespace eventflow istio-injection=enabled --overwrite
kubectl apply -f infrastructure/istio/
```

This applies, in one pass since none of them depend on ordering among themselves:

- `gateway.yaml`: the ingress `Gateway` that is the only path into the mesh from outside.
- `virtual-services.yaml`: routing for every service; only `api-gateway`'s has a second, initially
  zero-weight route to a `canary` subset.
- `destination-rules.yaml`: connection pool limits, outlier detection, and the `stable`/`canary`
  subsets every service is ready for, by the `version` pod label.
- `peer-authentication.yaml`: namespace-wide strict mTLS between sidecars.
- `authorization-policies.yaml`: deny-by-default per workload; only api-gateway is reachable from
  outside the mesh, only order-service calls another backend directly (the synchronous stock
  reservation described in [saga-pattern.md](../architecture/saga-pattern.md)), everything else is
  reached only through api-gateway.
- `telemetry.yaml`: samples 10% of mesh requests into the shared tracing pipeline.

Namespace relabeling only affects pods created afterward; restart already-running pods
(`kubectl rollout restart deployment -n eventflow`) if Istio was installed after step 2.

## Canary releases

Only the API Gateway currently has a canary `Deployment`
(`infrastructure/k8s/base/api-gateway-canary.yaml`, starting at zero replicas) and a weighted
`VirtualService` route. Build and push the new version tagged `:canary` first, then run:

```bash
make canary-promote SERVICE=api-gateway
```

This calls `scripts/canary.sh promote api-gateway`, which:

1. Scales `api-gateway-canary` to match the stable Deployment's replica count.
2. Steps the `VirtualService` weight through 5, 25, 50 and 100 percent, pausing 30 seconds after
   each step (`STABILIZE_SECONDS`).
3. After each step, queries Prometheus for the canary subset's 5xx rate over the last minute and
   rolls back automatically if it exceeds 5% (`ERROR_RATE_THRESHOLD`).

The script reads Prometheus at `$PROMETHEUS_URL`, defaulting to `http://localhost:9090`; this
repository does not ship a Prometheus `Deployment` for Kubernetes yet (`infrastructure/k8s/base` has
none), so point `PROMETHEUS_URL` at whatever Prometheus already monitors the cluster, or
port-forward one you deploy separately, before promoting.

Roll back manually at any point with `make canary-rollback SERVICE=api-gateway`
(`scripts/canary.sh rollback api-gateway`), which sends all traffic back to `stable` and scales the
canary Deployment to zero.

## Verifying the rollout

```bash
kubectl get pods -n eventflow
kubectl get virtualservice,destinationrule -n eventflow
```

Every application Deployment exposes `/health/live` and `/health/ready` as its liveness and
readiness probes; a pod stuck `NotReady` is usually still waiting on its migration `Job` or on
postgres, redis or kafka becoming reachable.
