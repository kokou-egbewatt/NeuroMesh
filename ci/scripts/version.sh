#!/usr/bin/env bash
# Print the application version: the one in package.json, which
# `task ci:changelog` keeps equal to the newest CHANGELOG.md entry.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
grep -m1 -oE '"version":[[:space:]]*"[^"]+"' package.json | sed -E 's/.*"([^"]+)"$/\1/'
