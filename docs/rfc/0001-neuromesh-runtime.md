# RFC-0001: NeuroMesh Runtime (Phase 1)

- Status: Draft
- Date: 2026-07-03
- Authors: Kokou Egbewatt

## Problem

Teams running LLM inference today talk directly to a specific serving stack (Triton, vLLM, TensorRT-LLM, Ollama, SGLang) with no shared layer for routing, batching, retries, or streaming. Every consumer re-implements backoff, load balancing, and response streaming against a backend-specific API, and switching serving stacks means rewriting client code. NeuroMesh Runtime is the unified inference gateway that removes this: one gRPC contract in front of any supported backend.

## Proposal

Two services, communicating over gRPC defined in `proto/neuromesh/v1/runtime.proto`:

- **`services/runtime`**: implements `InferenceService`. Owns the backend abstraction (which serving stack handles a given model), batching, streaming, and retry logic. In this first cut it exposes:
  - `Route(RouteRequest) returns (RouteResponse)`: unary inference call.
  - `StreamInference(StreamRequest) returns (stream StreamChunk)`: server-streaming token output.
  - Backed by a stub handler for now. Real backend adapters (Triton/vLLM/etc.) are follow-up RFCs per backend.

- **`services/gateway`**: the traffic entrypoint. Terminates client connections (HTTP for now, exposing `/healthz`), applies auth/quota/rate-limit concerns (stubbed initially), and forwards to `services/runtime` via the generated Go client in `sdk/go`.

Both services are independent Go modules under the root `go.work`, each with its own `cmd/server/main.go`, `Dockerfile`, and `Taskfile.yml` (`build`, `run`, `test`). `sdk/go` holds the generated protobuf/gRPC stubs plus a thin hand-written client wrapper so callers don't construct gRPC clients by hand.

## Scope

In scope for this RFC's initial implementation:
- proto contract for unary + streaming inference
- `runtime` and `gateway` binaries that build, run, and pass a gRPC health check
- Go SDK client wrapper

Explicitly deferred:
- Real backend adapters (Triton/vLLM/TensorRT-LLM/Ollama/SGLang). Separate RFCs per adapter.
- Batching and load-balancing logic. Currently a stub pass-through.
- Auth, quotas, and rate limiting in `gateway`. Currently unimplemented.
- Python/TypeScript/Rust SDKs. Tracked as later follow-ups once the Go contract stabilizes.

## Alternatives considered

- **Skip the gateway, let clients call `runtime` directly**: rejected because auth, quotas, and rate limiting need a single enforcement point, and we don't want every backend adapter reimplementing them.
- **REST instead of gRPC**: rejected per the stated tech stack (gRPC and Go concurrency is the intended systems-depth signal for this phase). Streaming token output also maps more naturally onto gRPC server-streaming than chunked HTTP.

## Open questions

- Final shape of `RouteRequest`/`RouteResponse` once real backend adapters exist (model selection, routing hints, KV-cache locality metadata from the future `scheduler`).
- Whether `gateway` should also expose gRPC-web directly for the dashboard, or go through a separate edge proxy.
- Auth mechanism for `gateway` (mTLS between services vs. token-based). Likely its own ADR once `packages/auth` is designed.
