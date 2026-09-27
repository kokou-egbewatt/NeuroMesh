# local

The local environment is a single-node k3s cluster inside a WSL2 Ubuntu distro, with a schedulable NVIDIA GPU, a local image registry the node's containerd uses as a docker.io mirror, and a Windows relay for the ports the host reaches. Not docker-compose and not kind: both were measured unable to schedule the GPU under WSL2.

Services deploy to it as the Helm charts under `infra/helm/`, selected by deploy profiles; this directory holds the profiles file and local values overlays. The cluster tooling is [#21](https://github.com/kokou-egbewatt/NeuroMesh/issues/21), the observability stack is [#22](https://github.com/kokou-egbewatt/NeuroMesh/issues/22), and the substrate ADR is [#23](https://github.com/kokou-egbewatt/NeuroMesh/issues/23).
