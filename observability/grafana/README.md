# grafana

Grafana comes with the VictoriaMetrics stack ([`infra/helm/victoria-metrics/`](../../infra/helm/victoria-metrics/)): the VictoriaMetrics datasource (uid `victoriametrics`) is provisioned as code, and the sidecars load datasources and dashboards from labelled ConfigMaps. VictoriaLogs and Tempo add their datasources when the logs and traces half of the profile lands. Nothing typed into the UI is kept: there is no persistence. Dashboard JSON is described in [`../dashboards/`](../dashboards/).
