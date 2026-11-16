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

  # The logs and traces half. The edge's Envoy reaches VictoriaLogs from the node's address, which
  # changes when WSL restarts, so its NetworkPolicy takes it from the cluster each time.
  local node_ip
  node_ip=$(k get node -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
  helm repo add grafana https://grafana.github.io/helm-charts --force-update >/dev/null
  helm repo add otel https://open-telemetry.github.io/opentelemetry-helm-charts --force-update >/dev/null

  echo "VictoriaLogs"
  helm dependency build infra/helm/victoria-logs >/dev/null
  h upgrade --install victorialogs infra/helm/victoria-logs -n "$NS" --set networkPolicy.nodeIP="$node_ip" \
    --wait --timeout 5m | grep -E "^STATUS"

  echo "Tempo"
  helm dependency build infra/helm/tempo >/dev/null
  h upgrade --install tempo infra/helm/tempo -n "$NS" --wait --timeout 5m | grep -E "^STATUS"

  echo "OpenTelemetry collector (agent and gateway)"
  helm dependency build infra/helm/otel-collector >/dev/null
  h upgrade --install otel infra/helm/otel-collector -n "$NS" --wait --timeout 5m | grep -E "^STATUS"
  # The pipelines are this chart's own ConfigMaps, which the upstream chart doesn't hash into its pod
  # template, so a changed pipeline needs a restart to load.
  k -n "$NS" rollout restart ds/otel-agent-agent deploy/otel-gateway >/dev/null
  k -n "$NS" rollout status ds/otel-agent-agent --timeout=3m >/dev/null
  k -n "$NS" rollout status deploy/otel-gateway --timeout=3m >/dev/null

  status
}

down() {
  need_cluster
  # The CRDs and the namespace stay, so service charts with a VMServiceScrape still apply. VMSingle's
  # volume goes with its CR; `task obs:up` starts the metrics history again.
  h uninstall otel -n "$NS" --ignore-not-found
  h uninstall tempo -n "$NS" --ignore-not-found
  h uninstall victorialogs -n "$NS" --ignore-not-found
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
  "Logs (vmui)|victorialogs.neuromesh.localhost|/select/vmui/|http://victorialogs-server:9428|||"
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

# The logs and traces path, end to end on the running cluster (#22): a request's log line reaches
# VictoriaLogs through the collector, a trace reaches Tempo, and the stores refuse writers other than
# the collector gateway. Test pods wait 15 s first: k3s's policy engine (kube-router) recognises a new
# pod a few seconds after it starts, and refuses it until then.
verify() {
  need_cluster
  local ca=certs/dev/ca.crt id pw failed=0 out code
  local curl_opts=(-s --max-time 10 --ssl-no-revoke --cacert "$ca")
  id="obs-verify-$(date +%s)"
  pass() { echo "  ok    $*"; }
  fail() { echo "  FAIL  $*"; failed=1; }

  echo "logs: gateway -> agent -> collector gateway -> VictoriaLogs"
  curl "${curl_opts[@]}" -o /dev/null -H "X-Request-Id: $id" -d '{"model":"llama3","prompt":"hello there world"}' \
    "https://gateway.neuromesh.localhost/v1/route" || true
  out=""
  for _ in $(seq 1 12); do
    out=$(curl "${curl_opts[@]}" --get --data-urlencode "query=request_id:\"$id\"" \
      "https://victorialogs.neuromesh.localhost/select/logsql/query" || true)
    [ -n "$out" ] && break
    sleep 5
  done
  if [ -n "$out" ]; then
    pass "request_id $id is in VictoriaLogs"
    echo "$out" | grep -q '"log.file.path"' && fail "a field outside fields.yaml got through (log.file.path)" || pass "redacted to fields.yaml"
  else
    fail "request_id $id did not reach VictoriaLogs within 60 s"
  fi

  echo "traces and NetworkPolicy, from a pod in the neuromesh namespace"
  k -n neuromesh delete pod obs-verify --ignore-not-found --wait >/dev/null
  cat <<YAML | k apply -f - >/dev/null
apiVersion: v1
kind: Pod
metadata: { name: obs-verify, namespace: neuromesh, labels: { app.kubernetes.io/part-of: neuromesh } }
spec:
  restartPolicy: Never
  securityContext: { runAsNonRoot: true, runAsUser: 65532, seccompProfile: { type: RuntimeDefault } }
  initContainers:
    - name: wait-for-policy
      image: curlimages/curl:8.17.0
      command: [sleep, "15"]
      securityContext: { allowPrivilegeEscalation: false, capabilities: { drop: [ALL] } }
    - name: trace
      image: ghcr.io/open-telemetry/opentelemetry-collector-contrib/telemetrygen:v0.163.0
      args: [traces, --otlp-endpoint=otel-gateway.observability:4317, --otlp-insecure, --traces=1, --service=$id]
      securityContext: { allowPrivilegeEscalation: false, capabilities: { drop: [ALL] } }
  containers:
    - name: probe
      image: curlimages/curl:8.17.0
      command: [sh, -c]
      args:
        - |
          echo "victorialogs=\$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST --data '{"_msg":"forged"}' http://victorialogs-server.observability:9428/insert/jsonline)"
          echo "tempo=\$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST http://tempo.observability:4318/v1/traces)"
          echo "gateway=\$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST -H 'Content-Type: application/json' --data '{"resourceLogs":[]}' http://otel-gateway.observability:4318/v1/logs)"
      securityContext: { allowPrivilegeEscalation: false, capabilities: { drop: [ALL] } }
YAML
  local phase=""
  for _ in $(seq 1 40); do
    phase=$(k -n neuromesh get pod obs-verify -o jsonpath='{.status.phase}')
    [ "$phase" = Succeeded ] || [ "$phase" = Failed ] && break
    sleep 3
  done
  out=$(k -n neuromesh logs obs-verify -c probe 2>/dev/null || true)
  k -n neuromesh delete pod obs-verify --wait=false >/dev/null
  [[ "$out" == *"victorialogs=000"* ]] && pass "VictoriaLogs refuses a writer in neuromesh" || fail "VictoriaLogs accepted a write from neuromesh ($out)"
  [[ "$out" == *"tempo=000"* ]] && pass "Tempo refuses a writer in neuromesh" || fail "Tempo accepted a write from neuromesh ($out)"
  [[ "$out" == *"gateway=200"* ]] && pass "the collector gateway accepts OTLP from neuromesh" || fail "the collector gateway refused OTLP from neuromesh ($out)"

  pw=$(k -n "$NS" get secret vm-grafana -o jsonpath='{.data.admin-password}' | base64 -d)
  out=""
  for _ in $(seq 1 12); do
    out=$(curl "${curl_opts[@]}" -u "admin:$pw" \
      "https://grafana.neuromesh.localhost/api/datasources/proxy/uid/tempo/api/search?tags=service.name%3D$id&limit=1&start=$(($(date +%s) - 900))&end=$(date +%s)" || true)
    [[ "$out" == *"\"rootServiceName\":\"$id\""* ]] && break
    sleep 5
  done
  [[ "$out" == *"\"rootServiceName\":\"$id\""* ]] && pass "trace from service $id is in Tempo (through Grafana's datasource)" || fail "trace from $id did not reach Tempo within 60 s"

  for uid in victoriametrics victorialogs tempo; do
    code=$(curl "${curl_opts[@]}" -o /dev/null -w '%{http_code}' -u "admin:$pw" "https://grafana.neuromesh.localhost/api/datasources/uid/$uid/health" || true)
    [ "$code" = 200 ] && pass "Grafana datasource $uid is healthy" || fail "Grafana datasource $uid: $code"
  done
  return "$failed"
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
  verify) verify ;;
  test-rules) test_rules ;;
  *) die "usage: scripts/observability.sh up|down|status|access [--show]|verify|test-rules" ;;
esac
