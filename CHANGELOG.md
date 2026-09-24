# Changelog

All notable changes to NeuroMesh are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Every change to code, contracts, infrastructure or CI adds a version here; `task ci:version-gate`
enforces it and `task ci:changelog` checks the file's structure.

## [Unreleased]

## [0.2.0] - Local Developer Tooling (2026-09-24)

**The scaffold worked, and using it took two terminals, a curl flag nobody remembered, and a CI
run to find out that a file had CRLF line endings.** This release is the daily loop.

### Added

- **`task dev`** runs the runtime and the gateway together with live reload (air), prefixed
  output, and dev certificates minted on first run. A change in a service, a shared package or
  the SDK is served within seconds.
- **`task call:route`, `call:health` and `call:runtime`** call the local stack over TLS. The last
  one calls the runtime directly with `buf curl`, presenting the gateway's certificate, so the
  runtime is reachable without reflection.
- **`task certs:trust` and `certs:untrust`** add the dev CA to the current Windows user's trust
  store, so a browser and plain curl accept `https://localhost:8443`.
- **`task doctor`** checks Go, the pinned tools, Node and pnpm, Docker, act, certificates, hooks,
  `origin/main` and line endings, and says what to run for each gap.
- **`task go:test:docker`** runs the tests in a Linux container with the race detector, for
  machines without cgo.
- **`task test:unit`, `test:golden` and `cover:html`** for the fast loop, golden updates and a
  coverage report.
- **Git hooks** (`lefthook.yml`, `task hooks:install`): line endings, Go formatting, doc links,
  changelog and buf lint before a commit; the version gate and unit tests before a push.
- **`task ci:line-endings`** in the docs job: act copies the working tree, not the commit, so a
  CRLF file on disk breaks it even when the commit is clean.
- **`task release:next`** adds the next changelog version and sets `package.json` to match.
- **`task adr:new` and `rfc:new`** create the next numbered ADR or RFC in the house format.
- **Shared VS Code config**: recommended extensions, launch configs for both services and the
  current test package, and a compound that starts both.
- **The version on every run.** `task ci:summary` writes the version, its release title, whether
  main has released it, the commit and the Go version to the top of the GitHub job summary, once
  per workflow.
- **gofumpt and goimports** through golangci-lint, with `task go:fmt`.
- **`-config` flag and `log_format`** on both services. Relative paths in a config resolve
  against the config file's directory, so a service runs from any working directory. Local
  configs log text; everything else stays JSON.

### Changed

- Test output prints each package's share of the workspace and one merged coverage total,
  instead of repeating the full package list per line.
- **Actions run on Node 24** (checkout v7, setup-go v7, setup-node v7, pnpm/action-setup v6), and
  jobs are pinned to `ubuntu-24.04`, so the `ubuntu-latest` move to Ubuntu 26 on 2026-10-19 lands as
  a deliberate change rather than a silent one.
- **Go 1.26.8 everywhere** (`go.work`, every `go.mod`, both Dockerfiles). setup-go v7 sets
  `GOTOOLCHAIN=local`, which showed that buf v1.73.0 needs Go 1.26.7: CI had been quietly downloading
  a newer toolchain than the repository declared.

## [0.1.0] - The Phase 1 Scaffold (2026-09-24)

**The first commit of the platform itself.** Before this, `main` held a README and a license. This
release is the baseline every Phase 1 issue lands on: two services that talk to each other over
mutual TLS, a gateway that validates what it accepts, tests that boot both, and CI that runs the
same way on GitHub, under act and in a terminal
([#94](https://github.com/kokou-egbewatt/NeuroMesh/issues/94)).

### Added

- **The monorepo layout** ([ADR-0001](docs/adr/0001-monorepo-structure-and-phasing.md)):
  `apps/`, `services/`, `packages/`, `sdk/`, `proto/`, `infra/`, `deployments/`, `observability/`,
  `benchmarks/`, `test/`, `tools/`, `docs/`, tied together by `go.work` and `pnpm-workspace.yaml`.
  Every Go module builds on its own through `replace` directives, and `task deps:check` fails
  when a module is untidy or disagrees with its siblings on a shared version.
- **The inference contract**, `proto/neuromesh/v1/runtime.proto`: unary `Route` and
  server-streaming `StreamInference`, with generation parameters and token usage. Linted by buf;
  the generated Go code in `sdk/go/gen` is committed and `task proto:check` fails when it drifts.
- **`services/runtime`**: the gRPC `InferenceService` with a stub backend until a real adapter
  lands, the gRPC health service, a message size limit, reflection off by default, and bounded
  shutdown (health flips to NOT_SERVING, calls drain for `shutdown_timeout`, the rest are cut).
- **`services/gateway`**: the HTTPS data-plane entrypoint. `/healthz` and `POST /v1/route`, a
  body limit (413), required and bounded `model` and `prompt` (400 with the field and a code),
  one JSON error shape carrying the request ID, `X-Request-Id` on every response, header and
  idle timeouts, and the runtime's status mapped to HTTP without returning its message. Routing
  goes through the `internal/router` seam later routers plug into.
- **The control plane and data plane split** ([ADR-0002](docs/adr/0002-control-plane-data-plane-split.md)):
  the gateway never calls a management service in the request path.
- **TLS on every hop.** HTTPS at the edge, TLS 1.3 minimum. Mutual TLS from gateway to runtime,
  with `tls.allowed_client_names` restricting which certificate common names may call: chain
  verification says the caller is a NeuroMesh service, the list says which one. Plaintext needs
  `tls.insecure: true` and logs a warning.
- **Certificates in the cert-manager Secret layout** (`tls.crt`, `tls.key`, `ca.crt`). On a
  cluster cert-manager mints them; `task certs:dev` mints a throwaway set for runs outside one,
  and the tests mint their own with the same code (`packages/utils/certgen`).
- **`sdk/go`**: a typed client that requires an explicit transport (`WithTLS` or `WithInsecure`),
  applies a default deadline to unary calls, and retries `Unavailable` only. `ResourceExhausted`
  is never retried: it means the request is too large or the backend is saturated.
- **Shared packages**: `packages/config` (YAML with unknown keys rejected, env overrides, the TLS
  section), `packages/logging` (JSON slog at the configured level), `packages/middleware` (gRPC
  deadline and retry interceptors), `packages/utils/certgen`.
- **Tests**: gateway handler table tests with no sockets, runtime transport tests (plaintext dial,
  foreign CA, disallowed caller, expired certificate, oversized message, shutdown, reflection),
  TLS and config tests, and `test/integration`, which boots both services in-process over real
  TLS and checks golden responses.
- **Images** for both services on `distroless/static-debian12:nonroot`, running as uid 65532 and
  built without protoc. `task images:smoke` runs them together and serves `/v1/route` over HTTPS.
- **CI**: `ci.yml` (Go, proto, web), `docs.yml` (links, changelog, version gate) and `images.yml`
  (build and smoke test), each a thin wrapper over `task ci:*`. `.actrc` and `.act/` make
  `act pull_request -W .github/workflows/<file>.yml` run with no flags
  ([Local CI](docs/onboarding/local-ci.md)).
- **Docs gates**: every relative Markdown link resolves inside the repository, this file is
  structurally sound and matches `package.json`, and code changes carry a new version here.
- **Pinned tooling** in `scripts/install-tools.sh`: task, buf, protoc-gen-go, protoc-gen-go-grpc
  and golangci-lint, used by CI, act and the Taskfile alike.
- **Design docs**: the [architecture overview](docs/architecture/overview.md) with the seven-phase
  roadmap and the local k3s and VictoriaMetrics substrate, and
  [RFC-0001](docs/rfc/0001-neuromesh-runtime.md) for the runtime. The delivery backlog lives in
  GitHub issues, one milestone per phase.
- **Local only.** The local k3s cluster is the one environment; `deployments/dev` and the
  remote `infra/` directories are placeholders until
  [#90](https://github.com/kokou-egbewatt/NeuroMesh/issues/90), and nothing in CI deploys anywhere.
