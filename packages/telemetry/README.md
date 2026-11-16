# telemetry

Request metrics every NeuroMesh service exposes on an admin `/metrics` endpoint in Prometheus format, scraped by vmagent through the chart's `VMServiceScrape` (#10).

| Metric | Labels |
| --- | --- |
| `neuromesh_requests_total` | `service`, `rpc`, `code` |
| `neuromesh_request_errors_total` | `service`, `rpc`, `code` |
| `neuromesh_request_duration_seconds` (histogram) | `service`, `rpc` |

`rpc` is a route template (`/v1/route`) on the gateway and a full gRPC method on the runtime, never a raw path; `code` is the HTTP status or the gRPC code. Nothing from a request's content is a label, so cardinality is fixed. Each service decides which codes are errors: 5xx on the gateway, anything but OK on the runtime. Health checks are not counted.

- **`metrics.enabled`** (or `METRICS_ENABLED`) turns it on; off, nothing listens and `Observe` does nothing.
- **`metrics.addr`** (or `METRICS_ADDR`), default `:9090`: its own port, so it needs no TLS and no client certificate.
