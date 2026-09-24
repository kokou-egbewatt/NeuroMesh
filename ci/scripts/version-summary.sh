#!/usr/bin/env bash
# The version this run builds, at the top of the GitHub job summary: the
# package.json version, its CHANGELOG title, and whether main has released it.
# Without GITHUB_STEP_SUMMARY (a terminal), it prints the same table.
# --print-only prints without writing, for version-control.sh to compose.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

version="$(grep -m1 -oE '"version":[[:space:]]*"[^"]+"' package.json | sed -E 's/.*"([^"]+)"$/\1/')"
heading="$(grep -m1 -E "^## \[$version\] - " CHANGELOG.md | tr -d '\r' || true)"
title="$(echo "$heading" | sed -E 's/^## \[[^]]+\] - (.*) \(([0-9-]+)\)$/\1/')"
date="$(echo "$heading" | sed -E 's/^.*\(([0-9-]+)\)$/\1/')"

sha="$(git rev-parse --short HEAD)"
ref="${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-$(git rev-parse --abbrev-ref HEAD)}}"
go_version="$(grep -m1 '^go ' go.work | awk '{print $2}')"

# A default checkout has no origin/main; one shallow ref is enough to compare.
git rev-parse --verify --quiet origin/main >/dev/null || git fetch --quiet --depth=1 origin main:refs/remotes/origin/main 2>/dev/null || true
released="not on main yet"
if git rev-parse --verify --quiet origin/main >/dev/null; then
  main_version="$(git show origin/main:package.json 2>/dev/null | grep -m1 -oE '"version":[[:space:]]*"[^"]+"' | sed -E 's/.*"([^"]+)"$/\1/' || true)"
  if [[ "$main_version" == "$version" ]]; then
    released="released on main"
  elif [[ -n "$main_version" ]]; then
    released="new since main ($main_version)"
  fi
fi

summary="## NeuroMesh v$version${title:+: $title}

| | |
| --- | --- |
| Version | \`$version\`${date:+ ($date)} |
| Status | $released |
| Commit | \`$sha\` on \`$ref\` |
| Go | \`$go_version\` |
"

if [[ "${1:-}" == "--print-only" ]]; then
  echo "$summary"
  exit 0
fi
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  echo "$summary" >>"$GITHUB_STEP_SUMMARY"
fi
echo "$summary"
