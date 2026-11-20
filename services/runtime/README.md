# runtime

Unified inference abstraction layer fronting Triton, vLLM, TensorRT-LLM, Ollama, and SGLang. Owns batching, streaming, retries, and model abstraction. See [RFC-0001](../../docs/rfc/0001-neuromesh-runtime.md).

Serves `InferenceService` (`proto/neuromesh/v1/runtime.proto`) over a backend, plus the gRPC health service. `pkg/server` builds the server so tests boot it in-process.

## Backends

`internal/backend` holds the `Backend` interface and its implementations; `internal/service` serves `InferenceService` over one and maps its errors to gRPC codes, once for every adapter (#1). The HTTP adapters share one client and one status mapping (`http.go`).

```mermaid
flowchart LR
    caller["gateway<br/>mutual TLS"] --> server["pkg/server<br/>TLS, health, metrics, tracing"]
    server --> service["internal/service<br/>Route, StreamInference,<br/>errors to gRPC codes"]
    service --> admit["internal/admit<br/>cap in flight, bounded queue<br/>(batching, off by default)"]
    admit --> iface{{"Backend<br/>Complete, Stream"}}
    iface --> stub["stub"]
    iface --> ollama["Ollama adapter"] -->|"/api/generate"| ext["Ollama"]
    iface --> vllm["vLLM adapter"] -->|"/v1/chat/completions"| ext2["vLLM"]
    iface -.-> later["Triton, SGLang<br/>later"]
```

- **`stub`** (the default) answers `stub response for model <model>`. The golden files, the existing tests and the #13 baseline use it, so they need no model server.
- **`ollama`** calls Ollama's `/api/generate`, streaming for `StreamInference`. Model names are Ollama tags, passed as they are. `temperature`, `max_tokens` and `stop` become Ollama's `temperature`, `num_predict` and `stop`; zero means Ollama's default.
- **`vllm`** (#105) calls vLLM's OpenAI-compatible `/v1/chat/completions` with the prompt as one user message, so an instruct model gets its chat template as it does through Ollama. Streaming reads the server-sent events and asks for `include_usage`. Model names are vLLM's served names (the Hugging Face repo id in the chart). `temperature`, `max_tokens` and `stop` pass as they are; zero is left out, so vLLM uses the model's defaults.

| Backend says | gRPC code |
| --- | --- |
| unreachable, 502, 503, or the answer cut off | `Unavailable`, within `dial_timeout` when nothing listens |
| 404 (model not pulled or not served) | `NotFound` |
| 400 (including a prompt past vLLM's `max_model_len`) | `InvalidArgument` |
| the runtime's cap and its queue are full (batching on) | `ResourceExhausted`, at once, without calling the backend |
| the caller's deadline or cancellation | `DeadlineExceeded`, `Canceled` |
| anything else | `Internal` |

The runtime boots and reports SERVING whether or not its backend is up; each request says `Unavailable` until it is. There is no fallback to the stub. On the cluster, `task ollama:up` or `task vllm:up` installs one, and `BACKEND=ollama` or `BACKEND=vllm task svc:up` points the runtime at it.

## Batching

The backend batches on the GPU itself: vLLM runs every request in flight as one batch, and Ollama has parallel slots. What it does not do is stop taking work when its KV cache is full; past that, vLLM preempts and queues without limit, so throughput stays flat while every request slows down ([measured](../../benchmarks/inference/batching/README.md)). With `batching.enabled`, `internal/admit` caps the requests in flight on the backend and holds a bounded queue in front (#2):

- **The cap** is about the KV cache's tokens (vLLM's "GPU KV cache size" startup line) over prompt plus `max_tokens`. It counts per runtime replica, so replicas sharing one backend split it.
- **The queue** is first in, first out. A queued request that is cancelled or runs out of deadline leaves without taking a slot. A full queue answers `ResourceExhausted` at once, which the gateway returns as 429 `backend_saturated`.
- **Size the queue from the deadline:** a queued request waits about (queue / cap) answers, and that plus its own answer has to fit in the gateway's `upstream_timeout`.
- **A stream holds its slot** until its last chunk. Below the cap a request is not held: the cap adds about 70 ns.
- **Metrics:** `neuromesh_backend_in_flight`, `_queued`, `_max_in_flight`, `_queue_wait_seconds` and `_rejected_total`, labelled by backend.

## Transport

Mutual TLS, TLS 1.3 minimum. A caller must present a certificate chaining to `tls.ca_file`, and with `tls.allowed_client_names` set, its common name must be on the list (`[gateway]` by default): chain verification says the caller is a NeuroMesh service, the list says which one. A plaintext dial fails at the handshake. `tls.insecure: true` serves plaintext and logs a warning.

## Running

```sh
task certs:dev   # once, from the repository root
task run         # go run ./cmd/server (or `task dev` from the root for both, with live reload), gRPC on :50051
```

## Config

`configs/config.yaml` (local run) and `configs/config.container.yaml` (baked into the image, certificates at `/certs`). `-config` or `CONFIG_PATH` picks the file, and relative paths inside it resolve against its directory; `RUNTIME_ADDR` overrides the listen address. Unknown keys fail startup.

| Key | Default | Meaning |
| --- | --- | --- |
| `addr` | `:50051` | gRPC listen address |
| `log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `log_format` | `json` | `json` or `text`; the local config uses `text` |
| `shutdown_timeout` | `10s` | On SIGTERM health flips to NOT_SERVING, in-flight calls drain this long, the rest are cut |
| `max_recv_msg_bytes` | `8 MiB` | Larger messages fail with ResourceExhausted; keep above the gateway's `limits.max_body_bytes` |
| `reflection` | `false` | gRPC reflection, for debugging only |
| `tls.cert_file`, `tls.key_file`, `tls.ca_file` | | Server certificate and the CA client certificates must chain to |
| `tls.allowed_client_names` | | Common names allowed to call |
| `backend.kind` | `stub` | `stub`, `ollama` or `vllm`; anything else fails startup. `BACKEND_KIND` overrides it |
| `backend.ollama.base_url` | | Ollama's address, e.g. `http://ollama:11434`. `OLLAMA_BASE_URL` overrides it |
| `backend.ollama.dial_timeout` | `3s` | Connecting; an unreachable Ollama is `Unavailable` within it |
| `backend.ollama.response_header_timeout` | `60s` | The wait for an answer to start. A unary answer starts only when it is complete, so this bounds model load plus generation |
| `backend.vllm.base_url`, `dial_timeout`, `response_header_timeout` | | The same for vLLM, e.g. `http://vllm:8000`. `VLLM_BASE_URL` overrides the address |
| `batching.enabled` | `false` | Cap the requests in flight on the backend. `BATCHING_ENABLED` overrides it |
| `batching.max_in_flight` | | At least 1 when enabled. `BATCHING_MAX_IN_FLIGHT` overrides it |
| `batching.max_queue` | `0` | Requests that may wait for a slot; 0 rejects whatever finds the cap full. `BATCHING_MAX_QUEUE` overrides it |

## Image

```sh
docker build -f services/runtime/Dockerfile -t neuromesh-runtime .   # from the repository root
```

Distroless static, uid 65532. `/certs` must hold `tls.crt`, `tls.key` and `ca.crt`: a cert-manager Secret on a cluster, or `task images:smoke` for a local run of both images.
