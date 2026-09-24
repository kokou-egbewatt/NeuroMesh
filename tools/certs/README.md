# certs

`task certs:dev` mints a throwaway CA and one certificate per service into `certs/dev` (gitignored) for running NeuroMesh on a laptop outside a cluster:

```text
certs/dev/ca.crt
certs/dev/runtime/{tls.crt,tls.key,ca.crt}
certs/dev/gateway/{tls.crt,tls.key,ca.crt}
```

The per-service directories use the layout a cert-manager Secret mounts, so a service config is the same on a laptop and on a cluster. On a cluster, cert-manager issues these certificates; this tool never runs there. Leaves carry both server and client authentication usages and SANs for the service name, the `task images:smoke` container name, and loopback. Re-run with `-- -force` to rotate everything; the CA key is never written, so rotation is always a full reissue.
