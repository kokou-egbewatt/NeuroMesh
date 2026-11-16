# victoria-metrics

VictoriaMetrics is the metrics store: the `victoria-metrics-k8s-stack` chart (operator, VMSingle, vmagent, vmalert, Alertmanager, Grafana, node-exporter, kube-state-metrics) wrapped in [`infra/helm/victoria-metrics/`](../../infra/helm/victoria-metrics/) with the platform's values, `VMRule` alert rules, dashboards and an alert sink. VMSingle as a single binary is a topology decision, not a budget one: VMCluster on one node buys nothing.

```bash
task obs:up          # install or upgrade the profile (idempotent)
task obs:status      # pods, and every scrape target up or down
task obs:access      # hostnames, paths and logins (passwords masked; -- --show prints them)
task obs:verify      # logs reach VictoriaLogs, a trace reaches Tempo, the stores refuse other writers
task obs:test-rules  # unit test the alert rules
task obs:down        # uninstall; CRDs and the namespace stay
```

## Access and credentials

| UI | Hostname | Useful path | In-cluster |
| --- | --- | --- | --- |
| Grafana | `grafana.neuromesh.localhost` | `/d/neuromesh-platform-status` | `http://vm-grafana` |
| Metrics (vmui) | `vmsingle.neuromesh.localhost` | `/vmui/` | `http://vmsingle-vm:8428` |
| Alerts (vmalert) | `vmalert.neuromesh.localhost` | `/vmalert/alerts` (without `/vmalert/`, vmalert answers 400) | `http://vmalert-vm:8080` |
| Alertmanager | `alertmanager.neuromesh.localhost` | `/#/alerts` | `http://vmalertmanager-vm:9093` |
| Logs (vmui) | `victorialogs.neuromesh.localhost` | `/select/vmui/` | `http://victorialogs-server:9428` |

The hostnames are served by the local edge (`task edge:up`; [Local hostnames](../../docs/onboarding/local-cluster.md#local-hostnames)) over HTTPS, with the dev CA that `task certs:trust` trusts. The chart renders its HTTPRoutes only when the cluster has the Gateway API, and `task obs:access` lists a hostname as reachable only once a route serves it, so without the edge every UI says "not routed yet" and answers inside the cluster only.

`task obs:access` also writes the whole sheet, passwords included, to `~/.neuromesh/access.md`. Grafana is the only UI with a login. The user is `admin`; the password is random per install, kept in the Secret `observability/vm-grafana`, so it survives `task obs:up` and differs between machines. Straight from the Secret:

```bash
kubectl -n observability get secret vm-grafana -o jsonpath='{.data.admin-user}' | base64 -d
kubectl -n observability get secret vm-grafana -o jsonpath='{.data.admin-password}' | base64 -d
```

## How it's put together

- **Install order.** The VictoriaMetrics CRDs come from the `victoria-metrics-operator-crds` chart, installed before the stack, so a service chart's `VMServiceScrape` can apply before the operator is up.
- **Scraping.** Services expose Prometheus-format `/metrics`; vmagent scrapes them every 30 s through the `VMServiceScrape` in each chart under `infra/helm/`. k3s has no etcd, scheduler, controller-manager or proxy endpoint, so those scrapes and their rules are off.
- **GPU.** [`infra/helm/dcgm-exporter/`](../../infra/helm/dcgm-exporter/) runs NVIDIA's dcgm-exporter as a GPU observer (see the exception in [Local cluster](../../docs/onboarding/local-cluster.md#a-gpu-pod)), collecting every field the exporter ships a definition for. On the reference RTX 5060 under WSL2, 27 of those 55 fields report; the profiling fields (`DCGM_FI_PROF_*`, such as `SM_ACTIVE`) report nothing.
- **Retention.** 45 days, with a free-space floor instead of a size limit, which VMSingle does not have. The numbers are in [Local cluster](../../docs/onboarding/local-cluster.md#the-observability-profile).
- **Alerts.** Rules are plain rule groups in `infra/helm/victoria-metrics/files/rules/`, wrapped into `VMRule`s by the chart and unit tested by [`tests/`](tests/) with `vmalert-tool unittest`. vmalert evaluates them and Alertmanager routes everything to the alert sink, a pod that writes each alert as one JSON line on stdout, until there is a pager.
- **Deploy needs the network.** The stack's sync job fetches its default rules and dashboards from upstream at install time.
