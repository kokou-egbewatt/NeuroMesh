#!/usr/bin/env bash
# `task go:test:docker`: the Go tests in a Linux container with the race
# detector, the way CI runs them. For machines without cgo (Windows without
# gcc), where `task go:test` has to skip -race.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# The toolchain line is the Go that builds the repository (the go line is only
# the minimum language version).
go_version="$(grep -m1 '^toolchain go' go.work | sed 's/^toolchain go//')"
src="$(pwd)"
# Docker Desktop on Windows needs C:/..., not Git Bash's /c/...
if pwd -W >/dev/null 2>&1; then src="$(pwd -W)"; fi

# MSYS_NO_PATHCONV keeps Git Bash from rewriting /src into a Windows path.
MSYS_NO_PATHCONV=1 docker run --rm \
  -v "$src:/src" -w /src \
  -v neuromesh-gomod:/go/pkg/mod \
  -v neuromesh-gocache:/root/.cache/go-build \
  -e CI=1 \
  "golang:$go_version" \
  bash ci/scripts/go-workspace.sh test "$@"
