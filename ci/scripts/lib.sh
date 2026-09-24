#!/usr/bin/env bash
# Shared helpers for ci/scripts. Sourced, never run.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

# Workspace module directories, from go.work, without a leading ./
workspace_modules() {
  go work edit -json | grep '"DiskPath"' | sed -E 's/.*"DiskPath": "\.?\/?([^"]+)".*/\1/'
}

# ./<module>/... patterns for go build, vet, test and golangci-lint.
workspace_patterns() {
  workspace_modules | sed -E 's|^(.*)$|./\1/...|' | tr '\n' ' '
}
