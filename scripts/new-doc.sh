#!/usr/bin/env bash
# `task adr:new -- "Title"` / `task rfc:new -- "Title"`: create the next
# numbered ADR or RFC in the house format (ADR-0001, RFC-0001) and print its path.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

kind="${1:-}"
title="${2:-}"
if [[ ! "$kind" =~ ^(adr|rfc)$ || -z "$title" ]]; then
  echo "usage: task adr:new -- \"Title\"   or   task rfc:new -- \"Title\"" >&2
  exit 2
fi

dir="docs/$kind"
last="$(ls "$dir" 2>/dev/null | grep -oE '^[0-9]{4}' | sort -n | tail -1)"
next="$(printf '%04d' $((10#${last:-0} + 1)))"
slug="$(echo "$title" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-|-$//g')"
file="$dir/$next-$slug.md"
author="$(git config user.name || echo "")"
today="$(date +%F)"
label="$(echo "$kind" | tr '[:lower:]' '[:upper:]')"

if [[ "$kind" == adr ]]; then
  cat >"$file" <<EOF
# $label-$next: $title

- Status: Proposed
- Date: $today
- Authors: $author

## Context

What forces the decision, and what is true today.

## Decision

What we do, stated so it can be checked.

## Alternatives considered

- **Option**: why it lost.

## Consequences

What this buys, what it costs, and when to revisit it.
EOF
else
  cat >"$file" <<EOF
# $label-$next: $title

- Status: Draft
- Date: $today
- Authors: $author

## Problem

## Proposal

## Scope

In scope:

Explicitly deferred:

## Alternatives considered

## Open questions
EOF
fi
echo "$file"
