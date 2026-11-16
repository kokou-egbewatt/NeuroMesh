#!/usr/bin/env bash
# `task ci:charts`: every chart under infra/helm/ lints, renders, and renders to valid Kubernetes
# objects. Charts render with the Gateway API on, so their HTTPRoutes are checked too, and with the
# values a deploy script would set (an image tag where a chart takes one), so a missing value fails
# here, not on a cluster.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
set -euo pipefail

export HELM_REPOSITORY_CONFIG="${TMPDIR:-/tmp}/neuromesh-helm/repositories.yaml"
export HELM_REPOSITORY_CACHE="${TMPDIR:-/tmp}/neuromesh-helm/cache"
mkdir -p "$(dirname "$HELM_REPOSITORY_CONFIG")"

# CRDs not in the default schemas come from the community catalog; the operator-specific ones it
# lacks are reported as skipped in the summary rather than failing the job.
CATALOG='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'
API=(--api-versions gateway.networking.k8s.io/v1/HTTPRoute)

failed=0
# The collector chart carries a copy of the field allow-list (Helm reads only inside a chart).
if ! diff -q docs/observability/fields.yaml infra/helm/otel-collector/fields.yaml >/dev/null; then
  echo "❌ infra/helm/otel-collector/fields.yaml differs from docs/observability/fields.yaml: cp the docs one over"
  failed=1
fi
for chart in infra/helm/*/; do
  name=$(basename "$chart")
  [ -f "$chart/Chart.yaml" ] || continue
  # Charts deployed with an image tag (the services) need one to render.
  SET=()
  grep -qE "^  tag:" "$chart/values.yaml" 2>/dev/null && SET=(--set image.tag=ci)
  if grep -q '^dependencies:' "$chart/Chart.yaml"; then
    sed -n 's/.*repository: *\(https:.*\)/\1/p' "$chart/Chart.yaml" | sort -u | while read -r repo; do
      helm repo add "$(echo "$repo" | md5sum | cut -c1-8)" "$repo" --force-update >/dev/null
    done
    helm dependency build "$chart" >/dev/null
  fi
  if ! out=$(helm lint "$chart" "${SET[@]}" 2>&1); then
    echo "❌ $name: helm lint"; echo "$out"; failed=1; continue
  fi
  if ! rendered=$(helm template "$name" "$chart" -n "$name" "${SET[@]}" "${API[@]}" 2>&1); then
    echo "❌ $name: helm template"; echo "$rendered"; failed=1; continue
  fi
  if ! out=$(echo "$rendered" | kubeconform -strict -summary -ignore-missing-schemas \
    -schema-location default -schema-location "$CATALOG" 2>&1); then
    echo "❌ $name: kubeconform"; echo "$out"; failed=1; continue
  fi
  echo "✅ $name: $(echo "$out" | tail -1)"
done
exit "$failed"
