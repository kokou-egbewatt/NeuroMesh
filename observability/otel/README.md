# otel

OpenTelemetry collector pipelines: the agent DaemonSet (reads `/var/log/pods`, runs no redaction) and the gateway Deployment, which is the only writer to VictoriaMetrics, VictoriaLogs and Tempo and the redaction boundary. Its `allowed_keys` is rendered from the telemetry field allow-list (`docs/observability/fields.yaml`, once it exists).
