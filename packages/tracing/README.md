# tracing

OpenTelemetry tracing for NeuroMesh services (#10). `tracing.Setup(ctx, service, version, cfg)` installs the global TracerProvider and the W3C trace-context propagator; the services' HTTP and gRPC instrumentation (`otelhttp`, `otelgrpc`) picks both up.

- **`otel.endpoint`** (or `OTEL_EXPORTER_OTLP_ENDPOINT`) is the collector gateway's OTLP gRPC address. Empty turns tracing off: spans are no-ops and nothing is exported, but the propagator still passes an incoming trace on.
- **`otel.insecure`** (or `OTEL_EXPORTER_OTLP_INSECURE`) sends without TLS, as the in-cluster collector gateway expects.
- **`otel.sample_ratio`** keeps that share of new traces; 0 means all, the right default at local volume. A parent's decision is always followed.
- **`NewProvider`** builds a provider with any span processor, which tests use with an in-memory exporter.

What a span may carry past the collector is in [`docs/observability/fields.yaml`](../../docs/observability/fields.yaml). Request content never becomes a span attribute: no prompt, no request ID.
