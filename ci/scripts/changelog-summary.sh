#!/usr/bin/env bash
# The latest changelog updates, for the docs job's GitHub summary: anything
# under [Unreleased], then the newest release entry.
#
#   ci/scripts/changelog-summary.sh              # write to the job summary (print in a terminal)
#   ci/scripts/changelog-summary.sh --print-only
#   ci/scripts/changelog-summary.sh --version 0.2.0 --print-only   # one version's entry only
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

print_only=false
version=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --print-only) print_only=true ;;
    --version) version="$2"; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
  shift
done

# section HEADING_PREFIX: the lines after a `## ` heading starting with the
# prefix, up to the next `## `, with ### demoted to #### so they sit under
# this summary's own headings.
section() {
  awk -v v="$1" '
    { sub(/\r$/, "") }
    index($0, v) == 1 { on = 1; next }
    on && /^## / { exit }
    on { print }
  ' CHANGELOG.md | sed -E 's/^### /#### /' | sed -e '/./,$!d'
}

if [[ -n "$version" ]]; then
  out="$(section "## [$version]")"
else
  newest="$(grep -m1 -E '^## \[[0-9]+\.[0-9]+\.[0-9]+\] - ' CHANGELOG.md | tr -d '\r')"
  newest_version="$(echo "$newest" | sed -E 's/^## \[([^]]+)\].*/\1/')"
  newest_title="$(echo "$newest" | sed -E 's/^## \[[^]]+\] - //')"
  unreleased="$(section "## [Unreleased]")"
  out="## Latest changelog updates
"
  if [[ -n "${unreleased//[[:space:]]/}" ]]; then
    out+="
### Unreleased

$unreleased
"
  fi
  out+="
### $newest_version: $newest_title

$(section "## [$newest_version]")
"
fi

if ! $print_only && [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  echo "$out" >>"$GITHUB_STEP_SUMMARY"
fi
echo "$out"
