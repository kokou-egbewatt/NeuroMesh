#!/usr/bin/env bash
# Run a Go command across every module in go.work.
#   ci/scripts/go-workspace.sh build|vet|lint|fmt|test|unit
#
# go.work has no module at its root, so `./...` does not resolve from here and
# each module needs its own pattern. They come from go.work itself, so a module
# added there is built, vetted, linted and tested with no second list to update.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cmd="${1:?usage: go-workspace.sh build|vet|lint|fmt|test|unit}"
shift
read -r -a pkgs <<<"$(workspace_patterns)"

case "$cmd" in
  build) go build "${pkgs[@]}" ;;
  vet) go vet "${pkgs[@]}" ;;
  lint) golangci-lint run "${pkgs[@]}" ;;
  fmt) golangci-lint fmt "${pkgs[@]%/...}" ;;
  test | unit)
    # unit skips test/integration: no sockets, seconds, for the inner loop.
    if [[ "$cmd" == unit ]]; then
      mapfile -t pkgs < <(workspace_patterns | tr ' ' '\n' | grep -v -e '^./test/' -e '^$')
    fi
    # The race detector needs cgo. CI always has it and must run with it; a
    # Windows machine without gcc runs without it and says so.
    race=()
    if [[ "$(go env CGO_ENABLED)" == "1" ]] && command -v gcc >/dev/null 2>&1; then
      race=(-race)
    elif [[ -n "${CI:-}" ]]; then
      echo "❌ CI must run the race detector, and cgo or gcc is unavailable here." >&2
      exit 1
    else
      echo "⚠️  race detector unavailable (no cgo or gcc): running without -race; \`task go:test:docker\` has it" >&2
    fi
    # -coverpkg across the workspace so test/integration counts toward the
    # server packages it exercises, not only toward itself. Generated code and
    # the command-line tools are left out of the denominator.
    coverpkg="$(go list "${pkgs[@]}" | grep -vE '/gen/|/cmd/|/tools/' | paste -sd, -)"
    # Each package would otherwise print the whole -coverpkg list; the merged
    # total at the end is the number that means something.
    go test "${race[@]}" -count=1 -covermode=atomic -coverpkg="$coverpkg" -coverprofile=coverage.out "$@" "${pkgs[@]}" |
      sed -E 's/coverage: ([0-9.]+)% of statements in .*/coverage: \1% of the workspace/'
    echo "merged coverage: $(go tool cover -func=coverage.out | tail -1 | awk '{print $NF}')"
    ;;
  *)
    echo "unknown command: $cmd" >&2
    exit 2
    ;;
esac
