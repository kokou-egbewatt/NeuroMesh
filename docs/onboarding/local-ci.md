# Local CI

Every GitHub Actions workflow here runs locally with [act](https://github.com/nektos/act), and every job is a thin wrapper over a `task ci:*` target, so GitHub-hosted runners, act and a plain terminal execute the same commands. This is the same setup as neuvia-infra's local CI.

## Prerequisites

| Tool | Version | Install |
| --- | --- | --- |
| act | 0.2.89 or newer | `choco install act-cli`, or [nektos/act releases](https://github.com/nektos/act/releases/latest) |
| Docker Desktop | current | [docker.com](https://www.docker.com/products/docker-desktop/) |
| Go | 1.26.3 (from `go.work`) | [go.dev/dl](https://go.dev/dl/) |
| task | pinned in `scripts/install-tools.sh` | `task tools:install` after installing any task once, or `go install github.com/go-task/task/v3/cmd/task@v3.52.0` |

On Windows, run `task` and the scripts from Git Bash. PowerShell may resolve `bash` to WSL, which has neither this checkout's paths nor its toolchain.

## The workflows

| Workflow | Job | Runs | Needs |
| --- | --- | --- | --- |
| `ci.yml` | go | `task ci:go`: deps check, build, vet, golangci-lint, tests with `-race` and coverage | Go |
| `ci.yml` | proto | `task ci:proto`: buf lint, generated code drift, breaking changes against `origin/main` | Go, full history |
| `ci.yml` | web | `task ci:web`: `pnpm install --frozen-lockfile`, `pnpm -r lint` | Node 20, pnpm |
| `docs.yml` | docs | `task ci:docs`: Markdown links, changelog integrity, version gate | full history |
| `images.yml` | images | `task ci:images`: build both images, run them, serve `/v1/route` over HTTPS | Docker |

## Running

From the repository root. `.actrc` supplies the runner image, the event and the env file, so no flags are needed:

```sh
act pull_request -W .github/workflows/ci.yml
act pull_request -W .github/workflows/docs.yml
act pull_request -W .github/workflows/images.yml
act pull_request -W .github/workflows/ci.yml -j go     # one job
```

Or skip act and run the same gates directly:

```sh
task ci            # everything
task ci:go         # one job
task ci:docs-links # one gate
```

## How the repository is set up for act

- `.actrc` pins the runner image (`ghcr.io/catthehacker/ubuntu:act-latest`), the event file, and the env file, and enables `--use-new-action-cache` so executable bits survive the copy from a Windows checkout.
- `.act/event.json` is the pull request payload with base `main`. It pins no head ref, so a run gates the branch you have checked out.
- `.act/env.env` is empty on purpose. act otherwise reads `.env` from the repository root and injects it into the job, and GitHub has no `.env`; anything a job needs comes from the workflow itself.
- `.act/secrets.env` is gitignored. No job needs a secret today.
- `.gitattributes` forces LF, because act copies the working tree into a Linux container and bash refuses CRLF scripts.
- `images.yml` needs nothing bind-mounted: `ci/scripts/images-smoke.sh` copies certificates into the containers with `docker cp` and runs the client as a container on the same network, so the job container's paths never reach the Docker daemon.

## The gates

- **Links** (`task ci:docs-links`): every relative link in a Markdown file git tracks or would track resolves to an existing path, and none climbs out of the repository. External links are not checked.
- **Changelog** (`task ci:changelog`): `[Unreleased]` comes first, release headings read `## [x.y.z] - Title (YYYY-MM-DD)`, versions are unique, descending and gap-free, no title heads two blocks, and `package.json` declares the newest version.
- **Version gate** (`task ci:version-gate`): a change under `apps`, `services`, `packages`, `sdk`, `proto`, `infra`, `deployments`, `tools`, `test`, `ci`, `scripts` or `.github` (Markdown excluded) against `origin/main` adds a new changelog version. It compares the working tree, untracked files included, so it gates a change before it is committed.
- **Dependencies** (`task deps:check`): every `go.mod` is in `go.work`, each module is tidy and builds with `GOWORK=off`, and `go work sync` changes nothing. `task deps:tidy` applies the fix.
- **Proto** (`task proto:check`, `task proto:breaking`): buf lint passes, `sdk/go/gen` is byte-identical to a fresh `buf generate`, and nothing breaks against `origin/main` once main has a buf module.

## Troubleshooting

- **`origin/main does not exist`**: run `git fetch origin main`. The docs and proto jobs compare against it.
- **`$'\r': command not found`**: an old checkout with CRLF scripts. Refresh it with `git rm --cached -r -q . && git reset --hard`.
- **`Permission denied` on an action script**: delete `%USERPROFILE%\.cache\act` so stale action copies are refetched with their modes.
- **`race detector unavailable`** locally on Windows: there is no gcc, so tests run without `-race`. CI always has it and fails if it does not.
- **Act Visual Runner mangles Windows paths**: neuvia-infra carries the patch (`make fix-act-runner` there); the terminal commands above are unaffected.
