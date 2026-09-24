# victoria-metrics

VictoriaMetrics is the metrics store: the `victoria-metrics-k8s-stack` chart (operator, VMSingle, vmagent, vmalert, Alertmanager, Grafana, node-exporter, kube-state-metrics) with the platform's values, `VMRule` alert rules, and the `VMServiceScrape` convention every service chart follows. VMSingle as a single binary is a topology decision, not a budget one: VMCluster on one node buys nothing.

Services expose Prometheus-format `/metrics`; vmagent scrapes them through the `VMServiceScrape` in each chart under `infra/helm/`. Rule unit tests use `vmalert-tool unittest`.
