# Observability

How metrics, logs, traces and alerts leave the services and reach Grafana. Each store has its own page under [`observability/`](../../observability/); the log and span fields allowed out are in [`fields.yaml`](../observability/fields.yaml).

## The flows

```mermaid
flowchart LR
    subgraph sources["neuromesh and the node"]
        svc["gateway, runtime"]
        node["kubelet, apiserver,<br/>node-exporter, kube-state-metrics"]
        dcgm["dcgm-exporter"]
    end

    subgraph collector["OTel collector"]
        agent["agent (DaemonSet)<br/>reads /var/log/pods"]
        cgw["gateway (Deployment)<br/>redacts to fields.yaml"]
    end

    subgraph stores["stores"]
        vmagent["vmagent"]
        vms[("VMSingle<br/>45 days")]
        vl[("VictoriaLogs<br/>7 days or 4 GiB")]
        tempo[("Tempo<br/>72 hours")]
    end

    subgraph alerting["alerting"]
        vmalert["vmalert<br/>VMRules"]
        am["Alertmanager"]
        sink["alert sink"]
    end

    graf["Grafana<br/>grafana.neuromesh.localhost"]

    svc -->|"stdout JSON"| agent
    agent -->|OTLP| cgw
    svc -->|"OTLP spans"| cgw
    cgw -->|logs| vl
    cgw -->|spans| tempo
    vmagent -->|"scrapes /metrics :9090"| svc
    vmagent --> node & dcgm
    vmagent --> vms
    vmalert -->|queries| vms
    vmalert --> am --> sink
    graf --> vms & vl & tempo
```

- **The collector gateway is the redaction boundary** and the only writer to VictoriaLogs and Tempo; their network policies admit nothing else.
- **Metrics are pulled**, not pushed: each chart ships a `VMServiceScrape`.
- **Alerts** go to the alert sink until there is a pager.

## One request across the stores

The request ID and the trace ID tie a request together across all three.

```mermaid
flowchart LR
    req["one request<br/>X-Request-Id"]
    trace["Tempo<br/>gateway HTTP span, client span,<br/>runtime server span"]
    logs["VictoriaLogs<br/>lines with trace_id, span_id,<br/>request_id"]
    metrics["VictoriaMetrics<br/>request counters and latency"]

    req --> trace
    req --> logs
    req --> metrics
    trace <-->|"trace_id"| logs
```

`task test:cluster` sends one request and finds it in all three stores.
