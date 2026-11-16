# tempo

Tempo is the trace store: a single binary on local disk from [`infra/helm/tempo/`](../../infra/helm/tempo/), fed by the collector gateway over OTLP. There is no object store on this cluster yet.

- **Query it** in Grafana (datasource uid `tempo`). A span links to its log lines in VictoriaLogs by `trace_id`; the service map reads span metrics from VictoriaMetrics once services emit them (#10, #65).
- **Retention:** 72 hours. Tempo has no size-based retention, and local-path doesn't enforce the 5Gi PVC, so the bound is time; its volume is measured in [Local cluster](../../docs/onboarding/local-cluster.md#the-observability-profile).
- **Who may connect:** a NetworkPolicy admits the collector gateway on OTLP (4317, 4318) and Grafana and vmagent on 3200. Services send traces to the collector gateway, never to Tempo.
- **Metrics generator** is off until the services trace (#10).
