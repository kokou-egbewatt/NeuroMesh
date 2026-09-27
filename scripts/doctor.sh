#!/usr/bin/env bash
# `task doctor`: check this machine can run every task, and say what to do when
# it cannot. Exits non-zero only for a missing requirement; warnings are for
# things that degrade a run (no race detector) rather than stop it.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

fails=0
warns=0
ok() { printf '  ✅ %s\n' "$*"; }
warn() { printf '  ⚠️  %s\n' "$*"; warns=$((warns + 1)); }
fail() { printf '  ❌ %s\n' "$*"; fails=$((fails + 1)); }

# version_ge A B: A >= B for dotted versions.
version_ge() { [[ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -1)" == "$2" ]]; }

echo "Shell"
if [[ -n "${WSL_DISTRO_NAME:-}" ]]; then
  ok "WSL ($WSL_DISTRO_NAME)"
elif [[ "$(uname -s)" == MINGW* || "$(uname -s)" == MSYS* ]]; then
  ok "Git Bash"
else
  ok "$(uname -s)"
fi

echo "Go"
if command -v go >/dev/null; then
  # go.work's go line is the minimum an installed Go (and gopls) needs; its
  # toolchain line is the Go that builds, which the go command downloads itself.
  min="$(grep -m1 '^go ' go.work | awk '{print $2}')"
  toolchain="$(grep -m1 '^toolchain go' go.work | sed 's/^toolchain go//')"
  installed="$(GOTOOLCHAIN=local go env GOVERSION | sed 's/^go//')"
  building="$(go env GOVERSION | sed 's/^go//')"
  if version_ge "$installed" "$min"; then ok "installed go $installed (gopls needs $min or newer)"; else fail "installed go $installed is older than $min from go.work: gopls cannot load the workspace"; fi
  if [[ "$(go env GOTOOLCHAIN)" == local* && "$building" != "$toolchain" ]]; then
    warn "GOTOOLCHAIN=local keeps the build on go $building; go.work pins go$toolchain"
  elif [[ "$building" == "$toolchain" ]]; then
    ok "builds with go $building (go.work toolchain)"
  else
    warn "builds with go $building, go.work pins go$toolchain"
  fi
  gobin="$(go env GOBIN)"
  [[ -z "$gobin" ]] && gobin="$(go env GOPATH)/bin"
  if command -v protoc-gen-go >/dev/null || command -v task >/dev/null; then
    ok "Go bin directory is on PATH"
  else
    warn "$gobin is not on PATH; pinned tools will not be found"
  fi
  if [[ "$(go env CGO_ENABLED)" == "1" ]] && command -v gcc >/dev/null; then
    ok "cgo and gcc: tests run with -race"
  else
    warn "no gcc: tests run without -race here; use \`task go:test:docker\` for CI parity"
  fi
else
  fail "go not found: https://go.dev/dl/"
fi

echo "Pinned tools (scripts/install-tools.sh)"
while read -r tool want; do
  if ! command -v "$tool" >/dev/null; then
    fail "$tool missing: run \`task tools:install\`"
    continue
  fi
  case "$tool" in
    task) have="$(task --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)" ;;
    buf) have="$(buf --version 2>/dev/null)" ;;
    protoc-gen-go) have="$(protoc-gen-go --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')" ;;
    protoc-gen-go-grpc) have="$(protoc-gen-go-grpc --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')" ;;
    golangci-lint) have="$(golangci-lint --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)" ;;
    air) have="$(air -v 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 | tr -d v)" ;;
    lefthook) have="$(lefthook version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')" ;;
    *) have="" ;;
  esac
  want="${want#v}"
  if [[ "$have" == "$want" ]]; then ok "$tool $have"; else warn "$tool ${have:-unknown}, pinned $want: run \`task tools:install\`"; fi
done < <(bash scripts/install-tools.sh --versions)

echo "Node and pnpm"
if command -v node >/dev/null; then
  have="$(node --version | tr -d v)"
  if version_ge "$have" "22.13.0"; then ok "node $have"; else fail "node $have: pnpm 11 needs 22.13 or newer"; fi
else
  fail "node not found (22.13 or newer)"
fi
if command -v pnpm >/dev/null; then
  want="$(grep -oE '"packageManager": *"pnpm@[^"]+"' package.json | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')"
  have="$(pnpm --version)"
  if [[ "$have" == "$want" ]]; then ok "pnpm $have"; else warn "pnpm $have, package.json wants $want"; fi
else
  fail "pnpm not found: https://pnpm.io/installation"
fi

echo "Docker"
if command -v docker >/dev/null; then
  if docker info >/dev/null 2>&1; then ok "docker daemon reachable"; else warn "docker daemon not running: images and act need it"; fi
else
  warn "docker not found: images and act need it"
fi
command -v act >/dev/null && ok "act $(act --version | awk '{print $3}')" || warn "act not found: needed only to run workflows locally"

echo "Repository"
[[ -f certs/dev/ca.crt ]] && ok "dev certificates in certs/dev" || warn "no dev certificates: run \`task certs:dev\`"
if [[ -f .git/hooks/pre-commit ]] && grep -q lefthook .git/hooks/pre-commit 2>/dev/null; then ok "git hooks installed"; else warn "git hooks not installed: run \`task hooks:install\`"; fi
git rev-parse --verify --quiet origin/main >/dev/null && ok "origin/main present" || warn "origin/main missing: run \`git fetch origin main\`"
if bash ci/scripts/check-line-endings.sh >/dev/null 2>&1; then
  ok "no CRLF files in the working tree"
else
  warn "CRLF files in the working tree (act will break): run \`task ci:line-endings\` for the list"
fi

echo
if [[ $fails -gt 0 ]]; then
  echo "❌ $fails problem(s), $warns warning(s)."
  exit 1
fi
echo "✅ ready ($warns warning(s))."
