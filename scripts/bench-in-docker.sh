#!/usr/bin/env bash
# `task bench:docker`: the stub-path baseline in a Linux container, the
# reference environment for benchmarks/inference/BASELINE.md. It runs the
# matrix twice and fails if any p50 drifts by 10% or more, the same check as
# `task bench:repeat`. Native Windows numbers are not comparable with these:
# the network stack and the scheduler differ (see BASELINE.md).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

go_version="$(grep -m1 '^toolchain go' go.work | sed 's/^toolchain go//')"
commit="$(git rev-parse --short HEAD)$(git diff --quiet HEAD -- || echo -dirty)"
src="$(pwd)"
# Docker Desktop on Windows needs C:/..., not Git Bash's /c/...
if pwd -W >/dev/null 2>&1; then src="$(pwd -W)"; fi

out=benchmarks/inference/results
bench="go run ./benchmarks/inference/cmd/bench -commit $commit $*"
# MSYS_NO_PATHCONV keeps Git Bash from rewriting /src into a Windows path.
MSYS_NO_PATHCONV=1 docker run --rm \
  -v "$src:/src" -w /src \
  -v neuromesh-gomod:/go/pkg/mod \
  -v neuromesh-gocache:/root/.cache/go-build \
  "golang:$go_version" \
  bash -c "set -e
    $bench -out $out/linux-run1.json
    $bench -out $out/linux-run2.json
    go run ./benchmarks/inference/cmd/bench -compare $out/linux-run1.json,$out/linux-run2.json"
