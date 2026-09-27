# gateway

Traffic entrypoint for NeuroMesh: auth, quotas, rate limiting, routing, streaming sessions ("Envoy for AI inference"). See [RFC-0001](../../docs/rfc/0001-neuromesh-runtime.md).

This is the data plane, not the control plane. It sits in the hot path of every inference request and only reads locally cached config (auth keys, quotas, routing policy) at request time. It must never make a synchronous call to `apps/control-plane` (or any other management-plane service) while handling a request. See [ADR-0002](../../docs/adr/0002-control-plane-data-plane-split.md).

## Surface

HTTPS only, TLS 1.3 minimum. Every response carries `X-Request-Id` (the client's, when it is 1 to 128 characters of `[A-Za-z0-9._-]`, otherwise a generated UUID) and `Content-Type: application/json`.

| Endpoint | Method | Behavior |
| --- | --- | --- |
| `/healthz` | GET, HEAD | `{"status":"ok"}` |
| `/v1/route` | POST | `{"model","prompt","temperature","max_tokens","stop","request_id"}` → `{"request_id","output","prompt_tokens","completion_tokens"}` |

Errors are always `{"error":{"code","message","request_id","field","limit"}}`. The runtime's status maps to HTTP (Unavailable 503, DeadlineExceeded 504, ResourceExhausted 429, InvalidArgument 400, NotFound 404, anything else 502); the runtime's own message is logged and never returned. Auth and rate limiting are not implemented yet (#6, #7).

## Running

```sh
task certs:dev   # once, from the repository root
task run         # go run ./cmd/server (or `task dev` from the root for both, with live reload), HTTPS on :8443
task call:route  # from the root; curl with the dev CA
```

Calling it by hand needs `--cacert certs/dev/ca.crt`, plus `--ssl-no-revoke` with Windows curl, which otherwise demands a revocation check a throwaway CA cannot answer. `task certs:trust` removes the need for `--cacert`.

## Config

`configs/config.yaml` (local run) and `configs/config.container.yaml` (baked into the image, certificates at `/certs`). `-config` or `CONFIG_PATH` picks the file, and relative paths inside it resolve against its directory; `GATEWAY_ADDR` and `RUNTIME_ADDR` override the listen and runtime addresses. Unknown keys fail startup.

| Key | Default | Meaning |
| --- | --- | --- |
| `addr` | `:8443` | HTTPS listen address |
| `log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `log_format` | `json` | `json` or `text`; the local config uses `text` |
| `shutdown_timeout` | `10s` | Drain time on SIGTERM |
| `tls.cert_file`, `tls.key_file` | | Edge certificate. `tls.insecure: true` serves plaintext and logs a warning |
| `http.read_header_timeout`, `read_timeout`, `idle_timeout`, `max_header_bytes` | `5s`, `30s`, `120s`, `64 KiB` | Connection bounds |
| `limits.max_body_bytes` | `1 MiB` | 413 past it; the runtime's `max_recv_msg_bytes` must stay above |
| `limits.max_prompt_bytes` | `512 KiB` | 400 past it |
| `limits.upstream_timeout` | `30s` | One call to the runtime |
| `runtime.addr`, `runtime.server_name` | `localhost:50051`, `runtime` | Runtime address and the SAN its certificate must carry |
| `runtime.tls.*` | | Client certificate and CA for mutual TLS to the runtime |

## Image

```sh
docker build -f services/gateway/Dockerfile -t neuromesh-gateway .   # from the repository root
```

Distroless static, uid 65532. `/certs` must hold `tls.crt`, `tls.key` and `ca.crt`: a cert-manager Secret on a cluster, or `task images:smoke` for a local run of both images.
