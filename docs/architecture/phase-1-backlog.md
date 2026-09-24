# Phase 1 backlog: NeuroMesh Runtime

Tracks what is left to take Phase 1 (`services/runtime`, `services/gateway`, `proto/`, `sdk/go`) from "builds and passes a health check" to "the unified inference gateway described in the phase roadmap" (routing, streaming, batching, retries, load balancing).

## Already done

RFC-0001's initial scope is complete: proto contract (`proto/neuromesh/v1/runtime.proto`), `services/runtime` and `services/gateway` build/run/pass a gRPC health check, `sdk/go` client wrapper with retry and default-deadline interceptors, config loading (`packages/config`), graceful shutdown, a router seam (`services/gateway/internal/router`), golangci-lint in CI, and ADR-0001/ADR-0002. None of the issues below duplicate that work.

## A. Core functionality (Phase 1's actual objective is not met without these)

- [ ] **P1-01: Real backend adapter.** `services/runtime`'s `Route`/`StreamInference` currently return a hardcoded stub string. Wire up one real backend, Ollama is the simplest to run locally, so the gateway proxies to an actual model instead of an echo.
- [ ] **P1-02: Batching.** The phase roadmap lists batching as a core `runtime` responsibility; there is none today. Buffer concurrent requests for the same model within a time/size window before dispatching to the backend.
- [ ] **P1-03: Load balancing across runtime replicas.** `router.Static` (added to fix the pluggability gap) always returns the same single backend. Add a second `Router` implementation (round robin or least loaded) once there is more than one `runtime` instance to route across.
- [ ] **P1-04: HTTP streaming endpoint.** `StreamInference` exists in the proto and runtime, but the gateway's HTTP surface only exposes unary `/v1/route`. Add an SSE or chunked-transfer `/v1/stream` endpoint so non-gRPC clients can actually use streaming.
- [ ] **P1-05: Application-level retry on backend errors.** `sdk/go`'s retry interceptor only covers transport-level `Unavailable`/`ResourceExhausted`. Once there are multiple backends (P1-03), add retry-against-a-different-instance for backend-level failures (e.g., model overloaded).

## B. Security and multi-tenancy (currently zero)

- [ ] **P1-06: Request-time auth.** No API key or JWT validation exists at the gateway. Per ADR-0002, this must validate against a locally cached key set, not call out to `apps/control-plane` synchronously.
- [ ] **P1-07: Rate limiting and quota enforcement.** No per-tenant or per-key limits exist. Token bucket at the gateway, enforced locally.
- [ ] **P1-08: Transport security between gateway and runtime.** `sdk/go/client.go` uses `insecure.NewCredentials()`. Fine for local dev; needs mTLS (or at minimum a shared token) before any real traffic.
- [ ] **P1-20: Input validation and request size limits.** The gateway has no max body size or prompt length enforcement today, a DoS risk for a public-facing endpoint.

## C. Observability (the whole platform's stated purpose, not yet dogfooded)

- [ ] **P1-09: Structured logging.** Both services use bare `log.Printf`. Switch to `slog` with consistent fields (`request_id`, `tenant_id`, `model`).
- [ ] **P1-10: Tracing and metrics.** No OpenTelemetry instrumentation exists yet despite `packages/tracing`/`packages/telemetry` being planned. Add gRPC client/server interceptors for basic spans plus request count, latency histogram, and error rate metrics, end to end from gateway through runtime.

## D. Testing and verification

- [ ] **P1-11: First integration test.** `test/integration/` is empty. Add a test that boots `runtime` + `gateway` and asserts the golden path over real gRPC (not mocks).
- [ ] **P1-12: Unit tests.** Neither service has a single test file today. Start with the gateway HTTP handlers and `router.Static`.
- [ ] **P1-13: First benchmark.** `benchmarks/inference/` is an empty placeholder despite benchmarks being called out as one of the most important folders in the original project plan. Record baseline latency/throughput for the current stub path so future backend adapters (P1-01) have something to compare against.
- [ ] **P1-14: Verify the Dockerfiles actually build.** Both `services/runtime/Dockerfile` and `services/gateway/Dockerfile` were rewritten to be self-contained but were never build-tested (Docker daemon was not running when this was done). Confirm both build and run correctly.

## E. Engineering hygiene

- [ ] **P1-15: Dependency version policy ADR.** `sdk/go`, `services/runtime`, `services/gateway`, `packages/config`, and `packages/middleware` each have independent `go.mod` files that currently agree on grpc/protobuf versions by luck, not policy. Write an ADR (or add Renovate grouping, see P1-17) before this drifts.
- [ ] **P1-16: Vulnerability scanning in CI.** No `govulncheck` or container image scanning exists. Add both to `.github/workflows/ci.yml`.
- [ ] **P1-17: Dependency update automation.** No Dependabot/Renovate config exists for `go.sum`/`pnpm-lock.yaml`.

## F. Documentation follow-ups (flagged, not yet written)

- [ ] **P1-18: ADR on session affinity vs. KV-cache locality.** Phase 2's `session-manager` (sticky routing for streaming) and Phase 4's `scheduler` (KV-cache-aware placement) both want to control which backend serves a request, for different reasons. Reconcile before Phase 2 starts.
- [ ] **P1-19: ADR on control-plane to gateway config distribution.** ADR-0002 established that the gateway must never call `apps/control-plane` synchronously, but left open how config actually flows (push vs. poll, shared store vs. streaming API). Needed once `apps/control-plane` exists.

## Suggested order

A (P1-01 through P1-05) is what actually makes this "Phase 1: unified inference gateway" rather than a health-checked stub; do it first. D (P1-11 through P1-14) should land alongside A, not after, so the growing surface area doesn't go untested. B, C, and E can proceed in parallel since they don't block each other. F is documentation and can happen anytime before the phase it blocks starts.
