#!/usr/bin/env bash
# CHANGELOG.md is structurally sound and agrees with the version the repository
# declares.
#
#   1. An [Unreleased] section exists and comes first.
#   2. Every release heading reads `## [x.y.z] - Title (YYYY-MM-DD)`.
#   3. No version appears twice.
#   4. Versions are strictly descending, newest first.
#   5. No version is skipped between two entries.
#   6. No release title heads two blocks (a merge that duplicated a section and
#      stripped one heading's `## [x.y.z]` prefix passes 3 to 5).
#   7. The root package.json version equals the newest entry.
#
# Checks 3 to 6 are neuvia-infra's, each added there after that failure
# happened. Ported from its ci/scripts/changelog-integrity.sh.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CHANGELOG="CHANGELOG.md"

if [[ ! -f "$CHANGELOG" ]]; then
  echo "❌ $CHANGELOG is missing."
  exit 1
fi

first_heading="$(grep -m1 -E '^## ' "$CHANGELOG" | tr -d '\r')"
if [[ "$first_heading" != "## [Unreleased]" ]]; then
  echo "❌ The first section must be '## [Unreleased]', found: $first_heading"
  exit 1
fi

mapfile -t VERSIONS < <(grep -oE '^## \[[0-9]+\.[0-9]+\.[0-9]+\]' "$CHANGELOG" | tr -d '\r' | sed -E 's/^## \[(.*)\]/\1/')
if [[ ${#VERSIONS[@]} -eq 0 ]]; then
  echo "❌ No released version headings found in $CHANGELOG."
  exit 1
fi

heading_re='^[0-9]+:## \[[0-9]+\.[0-9]+\.[0-9]+\] - .+ \([0-9]{4}-[0-9]{2}-[0-9]{2}\)$'
bad_heading="$(grep -nE '^## \[[0-9]+\.[0-9]+\.[0-9]+\]' "$CHANGELOG" | tr -d '\r' | grep -vE "$heading_re" || true)"
if [[ -n "$bad_heading" ]]; then
  echo "❌ Release headings must read '## [x.y.z] - Title (YYYY-MM-DD)':"
  echo "$bad_heading" | sed 's/^/   line /'
  exit 1
fi

DUPLICATES=$(printf '%s\n' "${VERSIONS[@]}" | sort | uniq -d)
if [[ -n "$DUPLICATES" ]]; then
  echo "❌ $CHANGELOG uses a version more than once:"
  printf '   %s\n' $DUPLICATES
  exit 1
fi

SORTED=$(printf '%s\n' "${VERSIONS[@]}" | sort -V -r)
if [[ "$(printf '%s\n' "${VERSIONS[@]}")" != "$SORTED" ]]; then
  echo "❌ $CHANGELOG versions are not in descending order:"
  echo "   found:    $(printf '%s ' "${VERSIONS[@]}")"
  echo "   expected: $(echo "$SORTED" | tr '\n' ' ')"
  exit 1
fi

for ((i = 0; i < ${#VERSIONS[@]} - 1; i++)); do
  NEWER="${VERSIONS[i]}"
  OLDER="${VERSIONS[i + 1]}"
  IFS=. read -r O_MAJOR O_MINOR O_PATCH <<<"$OLDER"
  if [[ "$NEWER" != "$((O_MAJOR + 1)).0.0" && "$NEWER" != "$O_MAJOR.$((O_MINOR + 1)).0" && "$NEWER" != "$O_MAJOR.$O_MINOR.$((O_PATCH + 1))" ]]; then
    echo "❌ $CHANGELOG skips a version: $OLDER is followed by $NEWER"
    echo "   Expected one of: $((O_MAJOR + 1)).0.0, $O_MAJOR.$((O_MINOR + 1)).0, $O_MAJOR.$O_MINOR.$((O_PATCH + 1))"
    exit 1
  fi
done

# Matched as a whole line, not a substring: an entry that discusses an earlier
# release names its title in prose, and that must not count.
count_title_lines() {
  awk -v t="$1" '
    {
      line = $0
      sub(/\r$/, "", line)
      sub(/^[[:space:]]*/, "", line)
      sub(/^## \[[0-9]+\.[0-9]+\.[0-9]+\][[:space:]]*-[[:space:]]*/, "", line)
      sub(/^-[[:space:]]*/, "", line)
      sub(/[[:space:]]*$/, "", line)
      if (line == t) count++
    }
    END { print count + 0 }
  ' "$CHANGELOG"
}
while IFS= read -r title; do
  [[ -z "$title" ]] && continue
  occurrences="$(count_title_lines "$title")"
  if [[ "$occurrences" -gt 1 ]]; then
    echo "❌ A release title heads $occurrences blocks in $CHANGELOG: $title"
    echo "   Usually a duplicated section, often with one copy's '## [x.y.z]' prefix stripped by a merge."
    exit 1
  fi
done < <(grep -oE '^## \[[0-9]+\.[0-9]+\.[0-9]+\] - .*$' "$CHANGELOG" | tr -d '\r' | sed -E 's/^## \[[0-9]+\.[0-9]+\.[0-9]+\][[:space:]]*-[[:space:]]*//')

declared="$(grep -m1 -oE '"version":[[:space:]]*"[^"]+"' package.json | sed -E 's/.*"([^"]+)"$/\1/')"
if [[ "$declared" != "${VERSIONS[0]}" ]]; then
  echo "❌ package.json declares $declared but the newest $CHANGELOG entry is ${VERSIONS[0]}."
  exit 1
fi

echo "✅ $CHANGELOG is sound: ${#VERSIONS[@]} entries, descending, no duplicates or gaps, newest ${VERSIONS[0]} matches package.json."
