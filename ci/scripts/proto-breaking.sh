#!/usr/bin/env bash
# No breaking proto change against the base branch, by buf's FILE rules.
#
# Skips, loudly, when the base has no buf module yet: there is nothing to break.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

base="${BASE_REF:-origin/main}"
if ! git rev-parse --verify --quiet "$base" >/dev/null; then
  echo "❌ $base does not exist. Run \`git fetch origin main\` (CI checks out with fetch-depth 0)."
  exit 1
fi
if ! git cat-file -e "$base:buf.yaml" 2>/dev/null; then
  echo "⏭️  $base has no buf.yaml yet, so there is no proto baseline to compare against. Skipping."
  exit 0
fi
buf breaking --against ".git#ref=$base"
echo "✅ no breaking proto changes against $base"
