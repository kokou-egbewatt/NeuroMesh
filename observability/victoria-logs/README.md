# victoria-logs

VictoriaLogs is the log store: a single binary from [`infra/helm/victoria-logs/`](../../infra/helm/victoria-logs/), in the `observability` namespace. Services log JSON to stdout; the collector agent reads `/var/log/pods` and the collector gateway, the redaction boundary, is the only writer here (see [`../otel/`](../otel/)).

- **Query it** at `https://victorialogs.neuromesh.localhost/select/vmui/` or with LogsQL in Grafana (datasource uid `victorialogs`). A request's lines: `request_id:"<id>"`.
- **Stream fields** are `service`, `k8s_namespace` and `k8s_pod`, from [`docs/observability/fields.yaml`](../../docs/observability/fields.yaml).
- **Retention:** 7 days or 4 GiB on disk, whichever comes first (`retentionDiskSpaceUsage`). The byte cap matters because k3s's local-path storage does not enforce the 5Gi PVC.
- **Who may connect:** a NetworkPolicy admits the collector gateway (writes), Grafana and vmagent (reads and scrapes), and the node's address for the edge. `task obs:verify` checks that a pod in another namespace can't write.
- **The Grafana plugin** (`victoriametrics-logs-datasource`, pinned in `infra/helm/victoria-metrics`) is fetched from the Grafana catalogue when Grafana starts, so a Grafana restart needs the network.
