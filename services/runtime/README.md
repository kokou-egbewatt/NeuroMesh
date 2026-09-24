# runtime

Unified inference abstraction layer fronting Triton, vLLM, TensorRT-LLM, Ollama, and SGLang. Owns batching, streaming, retries, and model abstraction. See [RFC-0001](../../docs/rfc/0001-neuromesh-runtime.md).

Serves `InferenceService` (`proto/neuromesh/v1/runtime.proto`) with a stub backend in `internal/stub` until #1 lands a real adapter, plus the gRPC health service. `pkg/server` builds the server so tests boot it in-process.

## Transport

Mutual TLS, TLS 1.3 minimum. A caller must present a certificate chaining to `tls.ca_file`, and with `tls.allowed_client_names` set, its common name must be on the list (`[gateway]` by default): chain verification says the caller is a NeuroMesh service, the list says which one. A plaintext dial fails at the handshake. `tls.insecure: true` serves plaintext and logs a warning.

## Running

```sh
task certs:dev   # once, from the repository root
task run         # go run ./cmd/server, gRPC on :50051
```

## Config

`configs/config.yaml` (local run) and `configs/config.container.yaml` (baked into the image, certificates at `/certs`). `CONFIG_PATH` picks the file; `RUNTIME_ADDR` overrides the listen address. Unknown keys fail startup.

| Key | Default | Meaning |
| --- | --- | --- |
| `addr` | `:50051` | gRPC listen address |
| `log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `shutdown_timeout` | `10s` | On SIGTERM health flips to NOT_SERVING, in-flight calls drain this long, the rest are cut |
| `max_recv_msg_bytes` | `8 MiB` | Larger messages fail with ResourceExhausted; keep above the gateway's `limits.max_body_bytes` |
| `reflection` | `false` | gRPC reflection, for debugging only |
| `tls.cert_file`, `tls.key_file`, `tls.ca_file` | | Server certificate and the CA client certificates must chain to |
| `tls.allowed_client_names` | | Common names allowed to call |

## Image

```sh
docker build -f services/runtime/Dockerfile -t neuromesh-runtime .   # from the repository root
```

Distroless static, uid 65532. `/certs` must hold `tls.crt`, `tls.key` and `ca.crt`: a cert-manager Secret on a cluster, or `task images:smoke` for a local run of both images.
