#!/usr/bin/env bash
# Version control for the pipeline: the first CI job, and every other CI job
# waits for it.
#
#   1. CHANGELOG.md is structurally sound and package.json declares its newest
#      version (changelog-integrity.sh).
#   2. A change to code, contracts, infrastructure or CI carries a new version
#      (version-gate.sh).
#   3. The job summary opens with the version this run builds and that
#      version's changelog entry, so a run page says what it is shipping.
#
# The summary is written even when a check fails, so a red run still says which
# version it was about. Without GITHUB_STEP_SUMMARY (a terminal) it is printed.
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
here="$(dirname "${BASH_SOURCE[0]}")"

integrity_out="$(bash "$here/changelog-integrity.sh" 2>&1)"
integrity=$?
echo "$integrity_out"
gate_out="$(bash "$here/version-gate.sh" 2>&1)"
gate=$?
echo "$gate_out"

# The version header (version, title, release status, commit, Go).
header="$(bash "$here/version-summary.sh" --print-only)"

# This version's entry, from the same extractor the docs job uses.
version="$(grep -m1 -oE '"version":[[:space:]]*"[^"]+"' package.json | sed -E 's/.*"([^"]+)"$/\1/')"
entry="$(bash "$here/changelog-summary.sh" --version "$version" --print-only)"

status_line() { if [[ $1 -eq 0 ]]; then echo "✅ $2"; else echo "❌ $2"; fi; }
checks="### Checks

- $(status_line $integrity "$(echo "$integrity_out" | grep -E '^(✅|❌)' | head -1 | sed -E 's/^(✅|❌) //')")
- $(status_line $gate "$(echo "$gate_out" | grep -E '^(✅|❌)' | head -1 | sed -E 's/^(✅|❌) //')")
"

summary="$header

$checks
### Changelog for $version

$entry
"

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  echo "$summary" >>"$GITHUB_STEP_SUMMARY"
else
  echo
  echo "$summary"
fi

[[ $integrity -eq 0 && $gate -eq 0 ]]
