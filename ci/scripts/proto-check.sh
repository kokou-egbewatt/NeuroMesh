#!/usr/bin/env bash
# The proto module lints, and the committed stubs in sdk/go/gen are exactly
# what the pinned plugins generate from proto/.
#
# Generated code is committed so the Dockerfiles and every consumer of sdk/go
# build without protoc, and so a module fetched on its own compiles. The cost is
# that it can drift from proto/; this is the check that it has not.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

buf lint
echo "✅ buf lint"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
buf generate -o "$tmp"
if ! diff -r "$tmp/sdk/go/gen" sdk/go/gen; then
  echo
  echo "❌ sdk/go/gen is stale or was generated with different plugin versions."
  echo "   Run \`task proto:gen\` (after \`task tools:install\`) and commit the result."
  exit 1
fi
echo "✅ sdk/go/gen matches proto/"
