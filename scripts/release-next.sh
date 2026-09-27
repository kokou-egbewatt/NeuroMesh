#!/usr/bin/env bash
# `task release:next -- <major|minor|patch> "Title"`: add the next version to
# CHANGELOG.md under [Unreleased] and set package.json to match, so the two
# edits `task ci:changelog` compares can never disagree.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

bump="${1:-}"
title="${2:-}"
if [[ ! "$bump" =~ ^(major|minor|patch)$ || -z "$title" ]]; then
  echo "usage: task release:next -- <major|minor|patch> \"Title\"" >&2
  exit 2
fi

current="$(grep -m1 -oE '^## \[[0-9]+\.[0-9]+\.[0-9]+\]' CHANGELOG.md | tr -d '#[] \r')"
IFS=. read -r major minor patch <<<"$current"
case "$bump" in
  major) next="$((major + 1)).0.0" ;;
  minor) next="$major.$((minor + 1)).0" ;;
  patch) next="$major.$minor.$((patch + 1))" ;;
esac
today="$(date +%F)"

awk -v heading="## [$next] - $title ($today)" '
  { print }
  !done && $0 ~ /^## \[Unreleased\]/ { print ""; print heading; print ""; print "### Added"; print ""; done = 1 }
' CHANGELOG.md >CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md
sed -i -E "0,/\"version\": *\"[^\"]+\"/s//\"version\": \"$next\"/" package.json

echo "✅ $current → $next: CHANGELOG.md has the heading, package.json matches. Fill in the entry."
