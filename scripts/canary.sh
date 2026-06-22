#!/usr/bin/env bash
#
# Shifts traffic between the stable and canary subsets of a service's Istio VirtualService,
# following the 5/25/50/100 percent steps from docs/architecture/zero-downtime-deployment.md.
#
# Usage:
#   canary.sh promote <service>          Step traffic to canary through 5, 25, 50, 100 percent
#   canary.sh set <service> <weight>     Patch the canary subset to an explicit weight (0-100)
#   canary.sh rollback <service>         Send all traffic back to stable and scale canary to zero
#
# Requires kubectl (pointed at the target cluster), jq and curl. Reads the canary subset's error
# rate from Prometheus at $PROMETHEUS_URL (default http://localhost:9090); this repo does not ship
# a Prometheus Deployment for Kubernetes yet, so port-forward one before running "promote".
#
# Only api-gateway currently has a canary Deployment and a weighted VirtualService route
# (infrastructure/k8s/base/api-gateway-canary.yaml, infrastructure/istio/virtual-services.yaml).

set -euo pipefail

NAMESPACE="${NAMESPACE:-eventflow}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"
STABILIZE_SECONDS="${STABILIZE_SECONDS:-30}"
ERROR_RATE_THRESHOLD="${ERROR_RATE_THRESHOLD:-0.05}"
STEPS=(5 25 50 100)

usage() {
  echo "Usage: $0 promote <service> | set <service> <weight> | rollback <service>" >&2
  exit 1
}

require_tools() {
  local tool
  for tool in kubectl jq curl; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      echo "error: $tool is required" >&2
      exit 1
    fi
  done
}

canary_replicas_for() {
  # Mirrors the stable Deployment's replica count so canary gets a comparable share of pods.
  local service="$1"
  kubectl get deployment "$service" -n "$NAMESPACE" -o jsonpath='{.spec.replicas}'
}

scale_canary() {
  local service="$1" replicas="$2"
  echo "--> Scaling ${service}-canary to ${replicas} replica(s)"
  kubectl scale deployment "${service}-canary" -n "$NAMESPACE" --replicas="$replicas"
  if [ "$replicas" -gt 0 ]; then
    kubectl rollout status "deployment/${service}-canary" -n "$NAMESPACE" --timeout=120s
  fi
}

set_weight() {
  local service="$1" weight="$2" stable
  stable=$((100 - weight))
  echo "--> Routing ${service}: stable=${stable}% canary=${weight}%"
  kubectl patch virtualservice "$service" -n "$NAMESPACE" --type=json -p="[
    {\"op\": \"replace\", \"path\": \"/spec/http/0/route/0/weight\", \"value\": ${stable}},
    {\"op\": \"replace\", \"path\": \"/spec/http/0/route/1/weight\", \"value\": ${weight}}
  ]"
}

canary_error_rate() {
  local service="$1" query result
  query="sum(rate(istio_requests_total{destination_service_name=\"${service}\",destination_version=\"canary\",reporter=\"destination\",response_code=~\"5..\"}[1m])) / sum(rate(istio_requests_total{destination_service_name=\"${service}\",destination_version=\"canary\",reporter=\"destination\"}[1m]))"
  result=$(curl -sf --data-urlencode "query=${query}" "${PROMETHEUS_URL}/api/v1/query" | jq -r '.data.result[0].value[1] // "0"')
  echo "$result"
}

rollback() {
  local service="$1"
  set_weight "$service" 0
  scale_canary "$service" 0
  echo "${service} canary rolled back"
}

promote() {
  local service="$1" replicas weight error_rate
  replicas=$(canary_replicas_for "$service")
  scale_canary "$service" "$replicas"

  for weight in "${STEPS[@]}"; do
    set_weight "$service" "$weight"
    echo "--> Waiting ${STABILIZE_SECONDS}s for traffic to stabilize"
    sleep "$STABILIZE_SECONDS"
    error_rate=$(canary_error_rate "$service")
    echo "--> Canary error rate at ${weight}%: ${error_rate}"
    if awk -v r="$error_rate" -v t="$ERROR_RATE_THRESHOLD" 'BEGIN { exit !(r > t) }'; then
      echo "error: canary error rate ${error_rate} exceeds threshold ${ERROR_RATE_THRESHOLD}, rolling back" >&2
      rollback "$service"
      exit 1
    fi
  done

  echo "${service} canary promoted to 100%"
}

main() {
  require_tools
  [ "$#" -ge 2 ] || usage
  local action="$1" service="$2"
  case "$action" in
    promote)
      promote "$service"
      ;;
    set)
      [ "$#" -eq 3 ] || usage
      set_weight "$service" "$3"
      ;;
    rollback)
      rollback "$service"
      ;;
    *)
      usage
      ;;
  esac
}

main "$@"
