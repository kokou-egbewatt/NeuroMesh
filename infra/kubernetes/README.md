# kubernetes

Raw manifests only for cluster prerequisites that are not a chart: namespaces, CRDs installed before their operator, the `nvidia` RuntimeClass, NetworkPolicy defaults. Everything with a lifecycle is a chart under `../helm/`.

- [`gpu/`](gpu/): the NVIDIA device plugin (with `runtimeClassName: nvidia`) and the admission policy that makes every GPU pod request `nvidia.com/gpu` and use the RuntimeClass together. `task cluster:start` places them in k3s's auto-deploy folder. On k3s the `nvidia` RuntimeClass itself is created by k3s when it finds the NVIDIA container toolkit, so there is no manifest for it here. See [Local cluster](../../docs/onboarding/local-cluster.md).
