#!/usr/bin/env bash
# No text file in the working tree has CRLF line endings.
#
#   ci/scripts/check-line-endings.sh            # every tracked file
#   ci/scripts/check-line-endings.sh a.sh b.go  # just these (the pre-commit hook)
#
# .gitattributes normalizes what is committed, but act copies the working tree,
# and a script saved with CRLF fails there with `set: pipefail: invalid option
# name` even though the commit is fine. grep -U keeps the carriage return on
# Windows, where plain grep strips it and finds nothing.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

if [[ $# -gt 0 ]]; then
  files=("$@")
else
  mapfile -t files < <(git ls-files)
fi

bad=()
for f in "${files[@]}"; do
  [[ -f "$f" ]] || continue
  if grep -IqU $'\r' "$f"; then bad+=("$f"); fi
done

if [[ ${#bad[@]} -gt 0 ]]; then
  echo "❌ CRLF line endings in ${#bad[@]} file(s):"
  printf '   %s\n' "${bad[@]}"
  echo "   Fix with: sed -i 's/\\r\$//' <file>"
  exit 1
fi
echo "✅ ${#files[@]} file(s) use LF line endings."
