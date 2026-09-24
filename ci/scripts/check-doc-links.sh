#!/usr/bin/env bash
# Every relative Markdown link resolves to a file that exists, and none climbs
# out of the repository.
#
# The backlog moved to GitHub issues and the observability placeholders were
# renamed in the same week; both left links pointing at paths that no longer
# exist, and a reviewer reads the sentence, not the href. External links are
# not checked: they fail for reasons unrelated to this repository being correct.
# Ported from neuvia-infra's ci/scripts/check-doc-links.sh.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# Does a repository-relative path climb above the root? Counted lexically:
# realpath and pwd disagree on Windows (C:/ versus /c/), which would make every
# path look external on the machine where the link is written.
climbs_out() {
  local depth=0 seg
  local IFS='/'
  for seg in $1; do
    case "$seg" in
      "" | .) ;;
      ..)
        depth=$((depth - 1))
        ((depth < 0)) && return 0
        ;;
      *) depth=$((depth + 1)) ;;
    esac
  done
  return 1
}

broken=0
checked=0
files=0
while IFS= read -r doc; do
  # Tracked but deleted in the working tree: it is leaving, nothing to check.
  [[ -f "$doc" ]] || continue
  files=$((files + 1))
  dir="$(dirname "$doc")"
  while IFS= read -r target; do
    [[ -z "$target" ]] && continue
    case "$target" in
      http://* | https://* | mailto:* | \#*) continue ;;
    esac
    checked=$((checked + 1))
    path="$dir/${target%%#*}"
    if climbs_out "$path"; then
      echo "❌ $doc -> $target"
      echo "   leaves the repository; link to https://github.com/<owner>/<repo>/blob/<ref>/<path> instead"
      broken=$((broken + 1))
    elif [[ ! -e "$path" ]]; then
      echo "❌ $doc -> $target"
      broken=$((broken + 1))
    fi
  done < <(grep -oE '\]\([^) ]+\)' "$doc" | sed -E 's/^\]\(//; s/\)$//' | sed -E 's/#.*$//')
# Tracked files plus untracked ones git does not ignore: a local run checks the
# same set CI will see after the commit, before the commit exists.
done < <(git ls-files --cached --others --exclude-standard -- '*.md' | sort -u)

if [[ "$broken" -gt 0 ]]; then
  echo
  echo "$broken broken link(s) across $files Markdown files."
  exit 1
fi
echo "✅ $checked relative link(s) across $files Markdown files resolve."
