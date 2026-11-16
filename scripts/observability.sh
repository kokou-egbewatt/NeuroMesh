#!/usr/bin/env bash
# The observability profile on the local cluster (#22): `task obs:up|down|status|access|test-rules`.
# This is the metrics half: the VictoriaMetrics stack and dcgm-exporter. observability/victoria-metrics/README.md
# and docs/onboarding/local-cluster.md say what it costs and what it keeps.
set -euo pipefail
export MSYS_NO_PATHCONV=1

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CONTEXT="${NEUROMESH_CONTEXT:-gpu-lab}"
NS=observability
VM_REPO=https://victoriametrics.github.io/helm-charts
CRDS_VERSION=0.15.2 # victoria-metrics-operator-crds, matching the operator in k8s-stack 0.97.1
VMALERT_TOOL=victoriametrics/vmalert-tool:v1.155.0

# Helm's own repo list, so a stale entry in the user's global config can't break a dependency build.
export HELM_REPOSITORY_CONFIG="${TMPDIR:-/tmp}/neuromesh-helm/repositories.yaml"
export HELM_REPOSITORY_CACHE="${TMPDIR:-/tmp}/neuromesh-helm/cache"

die() { echo "error: $*" >&2; exit 1; }
k() { kubectl --context "$CONTEXT" "$@"; }
h() { helm --kube-context "$CONTEXT" "$@"; }

need_cluster() {
  [ "$(kubectl config current-context 2>/dev/null)" = "$CONTEXT" ] || k get --raw /readyz >/dev/null 2>&1 \
    || die "no cluster at context $CONTEXT; run task cluster:start"
}

up() {
  need_cluster
  # dcgm-exporter only gets past the admission policy with its observer exemption.
  k get validatingadmissionpolicy gpu-request-and-runtimeclass-together \
    -o jsonpath='{.spec.variables[*].name}' 2>/dev/null | grep -qw gpuObserver \
    || die "the GPU admission policy has no observer exemption yet; run task cluster:start to update it"

  mkdir -p "$(dirname "$HELM_REPOSITORY_CONFIG")"
  helm repo add vm "$VM_REPO" --force-update >/dev/null

  echo "namespace and CRDs"
  k apply -f infra/kubernetes/observability/namespace.yaml >/dev/null
  h upgrade --install vm-crds vm/victoria-metrics-operator-crds --version "$CRDS_VERSION" -n "$NS" --wait | grep -E "^(STATUS|Error)" || true

  echo "VictoriaMetrics stack (first install pulls images; allow a few minutes)"
  helm dependency build infra/helm/victoria-metrics >/dev/null
  h upgrade --install vm infra/helm/victoria-metrics -n "$NS" --wait --timeout 10m | grep -E "^STATUS"

  echo "dcgm-exporter"
  h upgrade --install dcgm-exporter infra/helm/dcgm-exporter -n "$NS" --wait --timeout 5m | grep -E "^STATUS"

  status
}

down() {
  need_cluster
  # The CRDs and the namespace stay, so service charts with a VMServiceScrape still apply. VMSingle's
  # volume goes with its CR; `task obs:up` starts the metrics history again.
  h uninstall dcgm-exporter -n "$NS" --ignore-not-found
  h uninstall vm -n "$NS" --ignore-not-found
}

status() {
  need_cluster
  k -n "$NS" get pods -o wide
  echo
  # Every scrape target and whether it is up, straight from VMSingle.
  local q='sort_desc(max by (job) (up))'
  if out=$(k -n "$NS" exec deploy/vm-alert-sink -- python3 -c \
    "import urllib.request,urllib.parse,json;r=json.load(urllib.request.urlopen('http://vmsingle-vm:8428/api/v1/query?query='+urllib.parse.quote('$q')));print('\n'.join(f\"{'up  ' if x['value'][1]=='1' else 'DOWN'} {x['metric'].get('job','?')}\" for x in r['data']['result']))" 2>/dev/null); then
    echo "scrape targets:"
    echo "$out" | sed 's/^/  /'
  else
    echo "scrape targets: VMSingle not answering yet"
  fi
}

# What an operator reaches for: each UI's hostname and a useful path, and the logins.
# name | hostname | path | in-cluster URL | secret | user key | password key
ACCESS=(
  "Grafana|grafana.neuromesh.localhost|/d/neuromesh-platform-status|http://vm-grafana|vm-grafana|admin-user|admin-password"
  "Metrics (vmui)|vmsingle.neuromesh.localhost|/vmui/|http://vmsingle-vm:8428|||"
  "Alerts (vmalert)|vmalert.neuromesh.localhost|/vmalert/alerts|http://vmalert-vm:8080|||"
  "Alertmanager|alertmanager.neuromesh.localhost|/#/alerts|http://vmalertmanager-vm:9093|||"
)
ACCESS_FILE="${NEUROMESH_HOME:-$HOME/.neuromesh}/access.md"

# Prints the access sheet with passwords masked, and writes it in full to $ACCESS_FILE.
# -- --show prints the passwords too.
access() {
  need_cluster
  local show=${1:-} routed sheet line name host path internal secret ukey pkey user pw
  # A hostname is listed as reachable only once an HTTPRoute serves it (the local edge).
  routed=$(k get httproute -A -o jsonpath='{range .items[*]}{.spec.hostnames[*]}{" "}{end}' 2>/dev/null || true)
  sheet="# NeuroMesh access, $(date '+%Y-%m-%d %H:%M')"$'\n\nContext '"$CONTEXT"$'. Written by `task obs:access`; keep it off shared drives.\n'
  for line in "${ACCESS[@]}"; do
    IFS='|' read -r name host path internal secret ukey pkey <<<"$line"
    if [[ " $routed " == *" $host "* ]]; then
      echo "$name: https://$host$path"
      sheet+=$'\n'"## $name"$'\n\n'"- URL: https://$host$path"$'\n'
    else
      echo "$name: not routed yet (in-cluster $internal$path)"
      sheet+=$'\n'"## $name"$'\n\n'"- URL: not routed yet; in-cluster $internal$path"$'\n'
    fi
    [ -n "$secret" ] || continue
    user=$(k -n "$NS" get secret "$secret" -o jsonpath="{.data.$ukey}" 2>/dev/null | base64 -d || true)
    pw=$(k -n "$NS" get secret "$secret" -o jsonpath="{.data.$pkey}" 2>/dev/null | base64 -d || true)
    [ -n "$pw" ] || { echo "  login: secret $NS/$secret not found"; continue; }
    echo "  user $user, password $([ "$show" = --show ] && echo "$pw" || echo "******** (task obs:access -- --show, or $ACCESS_FILE)")"
    sheet+="- User: $user"$'\n'"- Password: $pw"$'\n'
  done
  mkdir -p "$(dirname "$ACCESS_FILE")"
  printf '%s' "$sheet" >"$ACCESS_FILE"
  chmod 600 "$ACCESS_FILE" 2>/dev/null || true
  echo
  echo "wrote $ACCESS_FILE"
}

test_rules() {
  # vmalert-tool in a container, so no binary to install. rule_files resolve from the test file.
  docker run --rm -v "$ROOT:/repo" -w /repo "$VMALERT_TOOL" unittest \
    --files=observability/victoria-metrics/tests/platform_test.yaml
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  status) status ;;
  access) access "${2:-}" ;;
  test-rules) test_rules ;;
  *) die "usage: scripts/observability.sh up|down|status|access [--show]|test-rules" ;;
esac
