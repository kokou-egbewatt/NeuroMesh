# dashboards

Dashboards as code: Grafana dashboard JSON in [`infra/helm/victoria-metrics/files/dashboards/`](../../infra/helm/victoria-metrics/files/dashboards/), which the chart ships as ConfigMaps carrying the sidecar label (`grafana_dashboard: "1"`) for the Grafana sidecar to load. They live in the chart because Helm can only read files inside it.

- **Platform status:** one row per scraped NeuroMesh and observability target, up or down, plus alerts firing.
- **GPU (DCGM):** memory, utilisation, power, temperature and clocks from dcgm-exporter, `GPU_UTIL` against `SM_ACTIVE`, and a table of every DCGM field with its last value.
