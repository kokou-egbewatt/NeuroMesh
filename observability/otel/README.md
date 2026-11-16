# otel

The OpenTelemetry collector, from [`infra/helm/otel-collector/`](../../infra/helm/otel-collector/): the contrib distribution, because the redaction processor isn't in the k8s one.

```text
pod stdout -> /var/log/pods -> agent (DaemonSet) ---------+
services' OTLP (#10) -------------------------------------+-> gateway (Deployment) -> VictoriaLogs
                                                                                   -> Tempo
```

- **The agent** reads every pod's log file as root (k3s writes them readable by root only), joins containerd's partial lines, takes namespace, pod and container from the file path, parses JSON lines into attributes, and forwards. It redacts nothing and skips its own and the gateway's logs.
- **The gateway** is the redaction boundary and the only writer to the stores. It copies the Kubernetes names onto each record as `k8s_namespace`, `k8s_pod` and `k8s_container`, then drops every attribute not in [`docs/observability/fields.yaml`](../../docs/observability/fields.yaml); dropped keys are counted (`redaction.redacted.count`). It applies the list to resource attributes too, which is why `service.name` is on it.
- **The allow-list** is rendered from `infra/helm/otel-collector/fields.yaml`, a copy of the docs file, since Helm reads only inside a chart; `task ci:charts` fails when they differ. An empty list fails the render, so a mistake fails closed.
- **Who may connect:** the gateway takes OTLP from the agent and from pods in `neuromesh`, nothing else.
- **Pipelines live in templates**, not values, so each reads as one config. The upstream chart doesn't hash them into the pod template, so `task obs:up` restarts both after an upgrade.
