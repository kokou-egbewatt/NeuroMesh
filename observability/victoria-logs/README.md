# victoria-logs

VictoriaLogs is the log store. Services log JSON to stdout; the OpenTelemetry collector agent reads `/var/log/pods` and the collector gateway is the only writer here. Retention is bounded by bytes as well as time because k3s local-path storage does not enforce PVC sizes. Queried with LogsQL, in Grafana through the VictoriaLogs datasource plugin (version pinned, fetched at pod start).
