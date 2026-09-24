#!/usr/bin/env bash
# A change to code, contracts, infrastructure or CI carries a new CHANGELOG.md
# version entry. Documentation-only changes do not need one.
#
# Compares the working tree (tracked changes and untracked files) with the
# merge base of BASE_REF, default origin/main, so a local run before the commit
# gates exactly what the PR will contain. Adapted from neuvia-infra's
# ci/scripts/version-gate.sh.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

base_ref="${BASE_REF:-origin/main}"
if ! git rev-parse --verify --quiet "$base_ref" >/dev/null; then
  echo "❌ $base_ref does not exist. Run 'git fetch origin main' (CI checks out with fetch-depth 0)."
  exit 1
fi
base="$(git merge-base "$base_ref" HEAD)"

CODE_PATHS=(apps services packages sdk proto infra deployments tools test ci scripts .github
  Taskfile.yml go.work buf.yaml buf.gen.yaml package.json pnpm-workspace.yaml pnpm-lock.yaml .golangci.yml)

changed="$(
  git diff --name-only "$base" -- "${CODE_PATHS[@]}"
  git ls-files --others --exclude-standard -- "${CODE_PATHS[@]}"
)"
changed="$(echo "$changed" | grep -vE '(^|/)[^/]*\.md$' | sed '/^$/d' || true)"
if [[ -z "$changed" ]]; then
  echo "✅ Only documentation changed since $base_ref. No version entry needed."
  exit 0
fi

versions() { grep -oE '^## \[[0-9]+\.[0-9]+\.[0-9]+\]' | tr -d '\r' | sort -u; }
base_versions="$(git show "$base:CHANGELOG.md" 2>/dev/null | versions || true)"
current_versions="$(versions <CHANGELOG.md 2>/dev/null || true)"
added="$(comm -13 <(echo "$base_versions") <(echo "$current_versions") | sed '/^$/d')"

if [[ -z "$added" ]]; then
  echo "❌ Code changed since $base_ref without a new CHANGELOG.md version entry. Changed:"
  echo "$changed" | head -20 | sed 's/^/   /'
  echo "   Add '## [x.y.z] - Title (YYYY-MM-DD)' under [Unreleased] and bump package.json to match."
  exit 1
fi
echo "✅ Code changed and CHANGELOG.md adds $(echo "$added" | tr '\n' ' ')"
