# local

The local environment is a single-node k3s cluster inside a WSL2 Ubuntu distro, with a schedulable NVIDIA GPU, a local image registry the node's containerd uses as a docker.io mirror, and a Windows relay for the ports the host reaches. `task cluster:start` brings it up; how to run it and why it is built this way is in [Local cluster](../../docs/onboarding/local-cluster.md).

k3s was chosen on measurement, not because the alternatives cannot reach the GPU. kind, k3d, minikube and Docker Desktop's Kubernetes can all schedule it under WSL2 with the right mounts, but k3s in the distro is about three times faster to a GPU pod and uses the least memory, and its GPU pods run unprivileged through the NVIDIA runtime (only minikube matches that, at six times the start-up time) ([infinity-bench #22](https://github.com/kokou-egbewatt/infinity-bench/issues/22)).

Services deploy to it as the Helm charts under `infra/helm/`, selected by deploy profiles; this directory holds the profiles file and local values overlays. The cluster tooling is [#21](https://github.com/kokou-egbewatt/NeuroMesh/issues/21), the observability stack is [#22](https://github.com/kokou-egbewatt/NeuroMesh/issues/22), and the substrate ADR is [#23](https://github.com/kokou-egbewatt/NeuroMesh/issues/23).
