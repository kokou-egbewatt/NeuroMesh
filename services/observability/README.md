# observability (service)

AI-specific telemetry on top of the platform stack (VictoriaMetrics, VictoriaLogs, Tempo, Grafana, OpenTelemetry collector, deployed in Phase 1): prompt lineage, token cost analytics, enrichment, and the query API the dashboard and CLI use. It consumes lifecycle events from the event fabric; it does not replace the stores. (Phase 5)
