# grafana

Grafana comes with the VictoriaMetrics stack ([`infra/helm/victoria-metrics/`](../../infra/helm/victoria-metrics/)). Its datasources are code: VictoriaMetrics (uid `victoriametrics`) from that chart, VictoriaLogs (`victorialogs`) and Tempo (`tempo`) as ConfigMaps from their own charts, which the sidecar loads. `task obs:verify` checks all three report healthy. Nothing typed into the UI is kept: there is no persistence. Dashboard JSON is described in [`../dashboards/`](../dashboards/).
