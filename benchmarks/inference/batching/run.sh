#!/usr/bin/env bash
# Runs sweep.py or load.py as a Job in the neuromesh namespace, in the vLLM image, and prints its
# result lines. vLLM must be up (`task vllm:up`); load.py also needs `BACKEND=vllm task svc:up`.
#
#   run.sh sweep [KEY=VALUE ...]        e.g. run.sh sweep OUT_LEN=1024 CONCS=64,128
#   run.sh load LABEL [KEY=VALUE ...]   e.g. run.sh load cap128-q16 CONCS=192,256
set -euo pipefail
cd "$(dirname "$0")"
CONTEXT="${NEUROMESH_CONTEXT:-gpu-lab}"
NS=neuromesh
IMAGE=vllm/vllm-openai:v0.12.0
k() { kubectl --context "$CONTEXT" -n "$NS" "$@"; }

script=${1:?usage: run.sh sweep|load [LABEL] [KEY=VALUE ...]}
shift
case "$script" in
  sweep) ;;
  load) set -- "LABEL=${1:?load needs a LABEL}" "${@:2}" ;;
  *) echo "unknown script $script: want sweep or load" >&2; exit 2 ;;
esac
job="batching-$script"

env=""
for kv in "$@"; do env+="            - { name: ${kv%%=*}, value: \"${kv#*=}\" }"$'\n'; done
# The sweep tokenizes with the model on vLLM's PVC.
data_mount="" data_volume=""
if [ "$script" = sweep ]; then
  data_mount="            - { name: data, mountPath: /data, readOnly: true }"
  data_volume="        - { name: data, persistentVolumeClaim: { claimName: vllm-data, readOnly: true } }"
fi

k create configmap "$job" --from-file=vllm_metrics.py --from-file="$script.py" -o yaml --dry-run=client | k apply -f - >/dev/null
k delete job "$job" --ignore-not-found --wait >/dev/null
k apply -f - >/dev/null <<YAML
apiVersion: batch/v1
kind: Job
metadata:
  name: $job
  labels: { app.kubernetes.io/name: $job, app.kubernetes.io/part-of: neuromesh }
spec:
  backoffLimit: 0
  ttlSecondsAfterFinished: 3600
  template:
    spec:
      restartPolicy: Never
      # A Service named vllm would inject VLLM_PORT, which the vllm CLI reads as its own setting.
      enableServiceLinks: false
      securityContext: { runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, seccompProfile: { type: RuntimeDefault } }
      containers:
        - name: run
          image: $IMAGE
          workingDir: /bench
          command: [python3, /bench/$script.py]
          env:
            - { name: HOME, value: /tmp }
            - { name: HF_HOME, value: /tmp/hf }
            - { name: HF_HUB_OFFLINE, value: "1" }
            - { name: USER, value: bench }
            - { name: VLLM_NO_USAGE_STATS, value: "1" }
$env          securityContext: { allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: [ALL] } }
          resources: { requests: { cpu: 500m, memory: 1Gi }, limits: { memory: 4Gi } }
          volumeMounts:
            - { name: bench, mountPath: /bench }
            - { name: ca, mountPath: /ca, readOnly: true }
            - { name: tmp, mountPath: /tmp }
$data_mount
      volumes:
        - { name: bench, configMap: { name: $job } }
        - { name: ca, configMap: { name: neuromesh-ca } }
        - { name: tmp, emptyDir: {} }
$data_volume
YAML

echo "$job running; results print as each step ends" >&2
k wait --for=condition=Ready pod -l job-name="$job" --timeout=5m >/dev/null
k logs -f "job/$job" | grep --line-buffered '^{'
k wait --for=condition=complete "job/$job" --timeout=60s >/dev/null || { k logs "job/$job" | tail -20 >&2; exit 1; }
