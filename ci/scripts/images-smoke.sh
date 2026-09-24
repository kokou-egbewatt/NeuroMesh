#!/usr/bin/env bash
# Build-verified images are not enough; this runs them. Both containers start
# on one network with freshly minted certificates, and the gateway must serve
# /v1/route over HTTPS through mutual TLS to the runtime, as uid 65532.
#
# Works on a laptop and under act alike because nothing is bind-mounted:
# certificates go in with `docker cp` and the client is a container on the same
# network, so the job container's paths and localhost never matter. On a
# cluster, cert-manager mounts the same three files at /certs from a Secret.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

tag="${IMAGE_TAG:-local}"
curl_image="curlimages/curl:8.11.1"
id="nm-smoke-$$"
net="$id"
tmp="$(mktemp -d)"

cleanup() {
  docker rm -f "$id-runtime" "$id-gateway" "$id-curl" >/dev/null 2>&1 || true
  docker network rm "$net" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

# 0644 keys: the files are owned by whoever runs this and read by uid 65532.
go run ./tools/certs -out "$tmp" -force -key-mode 0644 >/dev/null

docker network create "$net" >/dev/null
docker create --name "$id-runtime" --network "$net" --network-alias runtime "neuromesh-runtime:$tag" >/dev/null
docker cp "$tmp/runtime/." "$id-runtime:/certs"
docker create --name "$id-gateway" --network "$net" --network-alias gateway "neuromesh-gateway:$tag" >/dev/null
docker cp "$tmp/gateway/." "$id-gateway:/certs"
docker start "$id-runtime" "$id-gateway" >/dev/null

fail() {
  echo "❌ $*"
  echo "--- runtime logs ---"
  docker logs "$id-runtime" 2>&1 | tail -20
  echo "--- gateway logs ---"
  docker logs "$id-gateway" 2>&1 | tail -20
  exit 1
}

for c in runtime gateway; do
  user="$(docker inspect -f '{{.Config.User}}' "neuromesh-$c:$tag")"
  [[ "$user" == "nonroot:nonroot" || "$user" == "65532" || "$user" == "65532:65532" ]] || fail "$c image runs as '$user', want nonroot"
  proc_user="$(docker top "$id-$c" -o pid,user | tail -n +2 | awk '{print $2}' | head -1)"
  [[ -n "$proc_user" && "$proc_user" != "root" && "$proc_user" != "0" ]] || fail "$c process runs as '$proc_user'"
done
echo "✅ both containers run as a non-root user"

# The client: curl in a container on the same network, trusting only the dev CA.
body='{"model":"llama3","prompt":"hello there world"}'
# MSYS_NO_PATHCONV: Git Bash would rewrite the in-container /tmp/ca.crt into a
# Windows path. It is inert on Linux.
MSYS_NO_PATHCONV=1 docker create --name "$id-curl" --network "$net" "$curl_image" \
  --silent --show-error --fail-with-body --retry 20 --retry-connrefused --retry-delay 1 --max-time 10 \
  --cacert /tmp/ca.crt -H 'Content-Type: application/json' -H 'X-Request-Id: smoke-1' \
  -d "$body" https://gateway:8443/v1/route >/dev/null
docker cp "$tmp/ca.crt" "$id-curl:/tmp/ca.crt"
out="$(docker start -a "$id-curl" 2>&1)" || fail "HTTPS request failed: $out"
echo "$out"
[[ "$out" == *'"output":"stub response for model llama3"'* && "$out" == *'"request_id":"smoke-1"'* ]] || fail "unexpected response: $out"
echo "✅ /v1/route served over HTTPS, gateway to runtime over mutual TLS"

plain="$(docker run --rm --network "$net" "$curl_image" --silent --max-time 5 -o /dev/null -w '%{http_code}' http://gateway:8443/healthz 2>&1 || true)"
[[ "$plain" != "200" ]] || fail "plain HTTP was served"
echo "✅ plain HTTP is refused (got '$plain')"
