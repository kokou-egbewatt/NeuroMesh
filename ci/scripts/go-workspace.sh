#!/usr/bin/env bash
# Run a Go command across every module in go.work.
#   ci/scripts/go-workspace.sh build|vet|lint|test
#
# go.work has no module at its root, so `./...` does not resolve from here and
# each module needs its own pattern. They come from go.work itself, so a module
# added there is built, vetted, linted and tested with no second list to update.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cmd="${1:?usage: go-workspace.sh build|vet|lint|test}"
read -r -a pkgs <<<"$(workspace_patterns)"

case "$cmd" in
  build) go build "${pkgs[@]}" ;;
  vet) go vet "${pkgs[@]}" ;;
  lint) golangci-lint run "${pkgs[@]}" ;;
  test)
    # The race detector needs cgo. CI always has it and must run with it; a
    # Windows machine without gcc runs without it and says so.
    race=()
    if [[ "$(go env CGO_ENABLED)" == "1" ]] && command -v gcc >/dev/null 2>&1; then
      race=(-race)
    elif [[ -n "${CI:-}" ]]; then
      echo "❌ CI must run the race detector, and cgo or gcc is unavailable here." >&2
      exit 1
    else
      echo "⚠️  race detector unavailable (no cgo or gcc): running without -race" >&2
    fi
    # -coverpkg across the workspace so test/integration counts toward the
    # server packages it exercises, not only toward itself. Generated code and
    # the command-line tools are left out of the denominator.
    coverpkg="$(go list "${pkgs[@]}" | grep -vE '/gen/|/cmd/|/tools/' | paste -sd, -)"
    go test "${race[@]}" -count=1 -covermode=atomic -coverpkg="$coverpkg" -coverprofile=coverage.out "${pkgs[@]}"
    go tool cover -func=coverage.out | tail -1
    ;;
  *)
    echo "unknown command: $cmd" >&2
    exit 2
    ;;
esac
