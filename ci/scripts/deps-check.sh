#!/usr/bin/env bash
# Every Go module is tidy, builds on its own, and agrees with its siblings on
# shared dependency versions.
#
#   ci/scripts/deps-check.sh          # fail on drift, leave the fix in the tree
#   ci/scripts/deps-check.sh --fix    # the same, without failing
#
# `go work sync` pushes the workspace's selected version of every shared
# dependency into each module, and `go mod tidy` runs per module with
# GOWORK=off, which is what proves a module resolves its siblings through its
# own replace directives. If either changes a file, the committed state was
# drifting: grpc bumped in one module and not the others, a missing replace,
# a dependency marked // indirect that is imported directly.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

fix=false
[[ "${1:-}" == "--fix" ]] && fix=true

# Every go.mod in the repository is in go.work. A module left out builds
# nowhere and is tested by nobody.
missing=0
while IFS= read -r modfile; do
  dir="$(dirname "$modfile")"
  dir="${dir#./}"
  if ! workspace_modules | grep -qx "$dir"; then
    echo "❌ $dir has a go.mod but is not in go.work"
    missing=1
  fi
done < <(find . -name go.mod -not -path './node_modules/*' -not -path '*/testdata/*')
[[ $missing -eq 0 ]] || exit 1

snap="$(mktemp -d)"
trap 'rm -rf "$snap"' EXIT
files=(go.work)
[[ -f go.work.sum ]] && files+=(go.work.sum)
while IFS= read -r m; do
  files+=("$m/go.mod")
  [[ -f "$m/go.sum" ]] && files+=("$m/go.sum")
done < <(workspace_modules)
for f in "${files[@]}"; do
  mkdir -p "$snap/$(dirname "$f")"
  cp "$f" "$snap/$f"
done

go work sync
while IFS= read -r m; do
  (cd "$m" && GOWORK=off go mod tidy)
  (cd "$m" && GOWORK=off go build ./...)
done < <(workspace_modules)

drift=0
for f in "${files[@]}" $(workspace_modules | sed 's|$|/go.sum|'); do
  [[ -f "$f" || -f "$snap/$f" ]] || continue
  if ! diff -u "$snap/$f" "$f" >/dev/null 2>&1; then
    drift=1
    echo "❌ $f was not tidy:"
    diff -u "$snap/$f" "$f" || true
  fi
done

if [[ $drift -eq 1 ]]; then
  if $fix; then
    echo "✅ Tidied. Review and commit the changes above."
    exit 0
  fi
  echo
  echo "Dependencies drifted. Run \`task deps:tidy\` and commit the result."
  exit 1
fi
echo "✅ $(workspace_modules | wc -l | tr -d ' ') modules are tidy, build on their own, and agree on shared versions."
