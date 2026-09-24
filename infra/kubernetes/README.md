# kubernetes

Raw manifests only for cluster prerequisites that are not a chart: namespaces, CRDs installed before their operator, the `nvidia` RuntimeClass, NetworkPolicy defaults. Everything with a lifecycle is a chart under `../helm/`.
