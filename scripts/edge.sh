#!/usr/bin/env bash
# The local edge (#21, #23): `task edge:up|down|status`. One Envoy Gateway listener on 443 for
# *.neuromesh.localhost, so every UI on the local cluster has a hostname from Windows with no
# port-forward, relay or hosts file. docs/onboarding/local-cluster.md#local-hostnames explains it.
set -euo pipefail
export MSYS_NO_PATHCONV=1

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CONTEXT="${NEUROMESH_CONTEXT:-gpu-lab}"
NS=edge
EG_NS=envoy-gateway-system
EG_VERSION=v1.9.3
DOMAIN=neuromesh.localhost
CERTS=certs/dev

export HELM_REPOSITORY_CONFIG="${TMPDIR:-/tmp}/neuromesh-helm/repositories.yaml"
export HELM_REPOSITORY_CACHE="${TMPDIR:-/tmp}/neuromesh-helm/cache"

die() { echo "error: $*" >&2; exit 1; }
k() { kubectl --context "$CONTEXT" "$@"; }
h() { helm --kube-context "$CONTEXT" "$@"; }

need_cluster() { k get --raw /readyz >/dev/null 2>&1 || die "no cluster at context $CONTEXT; run task cluster:start"; }

up() {
  need_cluster
  [ -f "$CERTS/edge/tls.crt" ] || die "no edge certificate in $CERTS/edge; run task certs:dev -- -force, then task certs:trust (the CA is new, so trust it again)"

  echo "Envoy Gateway $EG_VERSION (with the Gateway API CRDs)"
  h upgrade --install eg oci://docker.io/envoyproxy/gateway-helm --version "$EG_VERSION" \
    -n "$EG_NS" --create-namespace --wait --timeout 5m | grep -E "^STATUS"

  echo "edge: namespace, certificate, Gateway"
  k create namespace "$NS" --dry-run=client -o yaml | k apply -f - >/dev/null
  k -n "$NS" create secret tls edge-tls --cert="$CERTS/edge/tls.crt" --key="$CERTS/edge/tls.key" \
    --dry-run=client -o yaml | k apply -f - >/dev/null
  h upgrade --install edge infra/helm/edge -n "$NS" --wait | grep -E "^STATUS"
  k -n "$NS" wait --for=condition=Programmed gateway/neuromesh-edge --timeout=180s >/dev/null

  # Charts render their HTTPRoutes only when the Gateway API exists; re-run the profile to add them.
  if h -n observability status vm >/dev/null 2>&1; then
    echo "observability: adding its routes"
    bash scripts/observability.sh up >/dev/null
  fi
  status
}

down() {
  need_cluster
  h uninstall edge -n "$NS" --ignore-not-found
  h uninstall eg -n "$EG_NS" --ignore-not-found
}

status() {
  need_cluster
  local programmed hosts host code
  programmed=$(k -n "$NS" get gateway neuromesh-edge -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' 2>/dev/null || true)
  echo "gateway neuromesh-edge programmed: ${programmed:-not installed}"
  hosts=$(k get httproute -A -o jsonpath='{range .items[*]}{.spec.hostnames[*]}{"\n"}{end}' 2>/dev/null | grep "\.$DOMAIN$" | sort -u || true)
  [ -n "$hosts" ] || { echo "no routes yet"; return; }
  # From Windows, over HTTPS, verified against the dev CA: what a browser would get.
  echo "from this machine, https and the dev CA:"
  for host in $hosts; do
    # --ssl-no-revoke: Windows curl (schannel) asks for a revocation list a dev CA doesn't publish.
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 --ssl-no-revoke --cacert "$CERTS/ca.crt" "https://$host/" || true)
    case "$code" in
      2??|3??) echo "  ok    https://$host ($code)" ;;
      000) echo "  FAIL  https://$host (no connection or TLS failure; is something else on :443?)" ;;
      *) echo "  FAIL  https://$host ($code)" ;;
    esac
  done
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  status) status ;;
  *) die "usage: scripts/edge.sh up|down|status" ;;
esac
