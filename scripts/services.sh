#!/usr/bin/env bash
# NeuroMesh's services on the local cluster (#14): `task image:runtime|gateway` and
# `task svc:up|down|status`. Images are built with Docker and imported straight into k3s's
# containerd, tagged with the commit, so there is no registry. docs/onboarding/local-cluster.md
# says what the profile costs.
set -euo pipefail
export MSYS_NO_PATHCONV=1

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CONTEXT="${NEUROMESH_CONTEXT:-gpu-lab}"
DISTRO="${NEUROMESH_DISTRO:-Ubuntu}"
NS=neuromesh
CERTS=certs/dev
DOMAIN=neuromesh.localhost

die() { echo "error: $*" >&2; exit 1; }
k() { kubectl --context "$CONTEXT" "$@"; }
h() { helm --kube-context "$CONTEXT" "$@"; }
need_cluster() { k get --raw /readyz >/dev/null 2>&1 || die "no cluster at context $CONTEXT; run task cluster:start"; }

# The commit, plus -dirty-<time> for uncommitted changes, so every build of different code is a new
# tag and the Deployment rolls to it (pullPolicy Never never re-reads a tag it already has).
tag() {
  local t
  t=$(git rev-parse --short=12 HEAD)
  [ -z "$(git status --porcelain -- services packages sdk go.work)" ] || t="$t-dirty-$(date +%s)"
  echo "$t"
}

# image <service> [tag]: build with Docker and import into k3s.
image() {
  local svc=$1 t=${2:-$(tag)} version
  version=$(bash ci/scripts/version.sh)
  echo "neuromesh-$svc:$t"
  docker build -q -f "services/$svc/Dockerfile" --build-arg "VERSION=$version" -t "neuromesh-$svc:$t" . >/dev/null
  docker save "neuromesh-$svc:$t" | wsl.exe -d "$DISTRO" -u root -- k3s ctr images import - >/dev/null
}

secret() {
  local svc=$1
  [ -f "$CERTS/$svc/tls.crt" ] || die "no certificate in $CERTS/$svc; run task certs:dev"
  k -n "$NS" create secret generic "$svc-tls" \
    --from-file=tls.crt="$CERTS/$svc/tls.crt" --from-file=tls.key="$CERTS/$svc/tls.key" \
    --from-file=ca.crt="$CERTS/$svc/ca.crt" --dry-run=client -o yaml | k apply -f - >/dev/null
}

up() {
  need_cluster
  local t
  t=$(tag)
  echo "images"
  image runtime "$t"
  image gateway "$t"

  echo "namespace, certificates"
  k apply -f infra/kubernetes/neuromesh/namespace.yaml >/dev/null
  secret runtime
  secret gateway
  # The CA the edge checks the gateway's certificate against.
  k -n "$NS" create configmap neuromesh-ca --from-file=ca.crt="$CERTS/ca.crt" \
    --dry-run=client -o yaml | k apply -f - >/dev/null

  echo "runtime, then gateway"
  h upgrade --install runtime infra/helm/runtime -n "$NS" --set image.tag="$t" --wait --timeout 3m | grep -E "^STATUS"
  h upgrade --install gateway infra/helm/gateway -n "$NS" --set image.tag="$t" --wait --timeout 3m | grep -E "^STATUS"
  status
}

down() {
  need_cluster
  h uninstall gateway -n "$NS" --ignore-not-found
  h uninstall runtime -n "$NS" --ignore-not-found
}

status() {
  need_cluster
  k -n "$NS" get pods -o wide
  echo
  local url="https://gateway.$DOMAIN"
  if k -n "$NS" get httproute gateway >/dev/null 2>&1; then
    # --ssl-no-revoke: Windows curl (schannel) asks for a revocation list a dev CA doesn't publish.
    local code
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 --ssl-no-revoke --cacert "$CERTS/ca.crt" "$url/healthz" || true)
    echo "$url/healthz: $code"
  else
    echo "$url: not routed (task edge:up adds the edge, then task svc:up again)"
  fi
}

# The route baseline from inside the cluster: the driver as a Job against the gateway Service, two
# runs and the 10% p50 repeatability check. From Windows the path would run through WSL's localhost
# relay and the edge, which on this machine drifted 38 to 77% between runs and measured the relay.
bench() {
  need_cluster
  local t commit job=bench out=benchmarks/inference/results args="[]"
  [ $# -eq 0 ] || args="[$(printf '"%s", ' "$@" | sed 's/, $//')]"
  t=$(tag)
  commit="$(git rev-parse --short HEAD)$(git diff --quiet HEAD -- || echo -dirty)"
  echo "neuromesh-bench:$t"
  docker build -q -f benchmarks/inference/Dockerfile -t "neuromesh-bench:$t" . >/dev/null
  docker save "neuromesh-bench:$t" | wsl.exe -d "$DISTRO" -u root -- k3s ctr images import - >/dev/null

  k -n "$NS" delete job "$job" --ignore-not-found --wait >/dev/null
  cat <<YAML | k apply -f - >/dev/null
apiVersion: batch/v1
kind: Job
metadata:
  name: $job
  namespace: $NS
  labels: { app.kubernetes.io/name: bench, app.kubernetes.io/part-of: neuromesh }
spec:
  backoffLimit: 0
  ttlSecondsAfterFinished: 3600
  template:
    metadata:
      labels: { app.kubernetes.io/name: bench, app.kubernetes.io/part-of: neuromesh }
    spec:
      restartPolicy: Never
      securityContext: { runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, seccompProfile: { type: RuntimeDefault } }
      containers:
        - name: bench
          image: neuromesh-bench:$t
          imagePullPolicy: Never
          args: $args
          env:
            - { name: TARGET, value: "https://gateway:8443" }
            - { name: CA, value: /ca/ca.crt }
            - { name: COMMIT, value: "$commit" }
          securityContext: { allowPrivilegeEscalation: false, readOnlyRootFilesystem: false, capabilities: { drop: [ALL] } }
          volumeMounts: [{ name: ca, mountPath: /ca, readOnly: true }]
      volumes:
        - name: ca
          configMap: { name: neuromesh-ca }
YAML
  echo "running two passes in the cluster (several minutes)"
  # Until the job succeeds or fails; kubectl wait can't wait for either of two conditions.
  local i done=""
  for i in $(seq 1 360); do
    done=$(k -n "$NS" get job "$job" -o jsonpath='{.status.succeeded}{.status.failed}')
    [ -n "$done" ] && break
    sleep 5
  done
  local log
  log=$(k -n "$NS" logs "job/$job")
  mkdir -p "$out"
  for run in 1 2; do
    echo "$log" | sed -n "/^--- json run$run\$/,/^--- end\$/p" | sed '1d;$d' >"$out/cluster-run$run.json"
  done
  echo "$log" | grep -v '^--- ' | grep -vE '^\s*[{}\[\]"]|^\s+"' || true
  [ "$(k -n "$NS" get job "$job" -o jsonpath='{.status.succeeded}')" = 1 ] || die "the runs were not repeatable, or the job failed (kubectl -n $NS logs job/$job)"
}

case "${1:-}" in
  up) up ;;
  bench) shift; bench "$@" ;;
  down) down ;;
  status) status ;;
  image) need_cluster; image "${2:?usage: scripts/services.sh image runtime|gateway}" ;;
  *) die "usage: scripts/services.sh up|down|status|bench [bench flags]|image <runtime|gateway>" ;;
esac
