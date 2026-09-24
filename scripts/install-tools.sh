#!/usr/bin/env bash
# The one place tool versions are pinned. CI, act, the Taskfile and the
# Dockerfiles all install through this script, so generated code and lint
# results do not depend on whose machine ran them.
#
#   bash scripts/install-tools.sh            # all tools
#   bash scripts/install-tools.sh buf task   # a subset
#   bash scripts/install-tools.sh --versions # print the pins
set -euo pipefail

declare -A PINS=(
  [task]="github.com/go-task/task/v3/cmd/task@v3.52.0"
  [buf]="github.com/bufbuild/buf/cmd/buf@v1.73.0"
  [protoc-gen-go]="google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12"
  [protoc-gen-go-grpc]="google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2"
  [golangci-lint]="github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2"
)
ORDER=(task buf protoc-gen-go protoc-gen-go-grpc golangci-lint)

if [[ "${1:-}" == "--versions" ]]; then
  for t in "${ORDER[@]}"; do echo "$t ${PINS[$t]##*@}"; done
  exit 0
fi

tools=("$@")
[[ ${#tools[@]} -eq 0 ]] && tools=("${ORDER[@]}")

bin="$(go env GOBIN)"
[[ -z "$bin" ]] && bin="$(go env GOPATH)/bin"
for t in "${tools[@]}"; do
  pkg="${PINS[$t]:-}"
  if [[ -z "$pkg" ]]; then
    echo "unknown tool: $t (known: ${ORDER[*]})" >&2
    exit 1
  fi
  echo "installing $t ${pkg##*@}"
  GOWORK=off go install "$pkg"
done

for t in "${tools[@]}"; do
  if ! command -v "$t" >/dev/null 2>&1; then
    echo "note: $t installed to $bin, which is not on PATH; add it so the Taskfile finds these tools" >&2
    break
  fi
done
