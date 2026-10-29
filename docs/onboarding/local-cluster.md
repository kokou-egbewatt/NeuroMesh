# Local cluster

NeuroMesh runs locally on one single-node k3s cluster inside a WSL2 distro, with a schedulable NVIDIA GPU. `task cluster:*` runs it ([`tools/cluster`](../../tools/cluster/main.go)), shaped by [`tools/cluster/cluster.yaml`](../../tools/cluster/cluster.yaml). Run every command from Git Bash at the repository root. Issue: [#21](https://github.com/kokou-egbewatt/NeuroMesh/issues/21).

## Commands

```sh
task cluster:start            # provision, or reconcile what is there, then wait for Ready and the GPU
task cluster:status           # every layer from the Windows driver to the pods; the first ❌ is the one to fix
task cluster:status -- -probe # also run a GPU pod and check the admission policy rejects bad shapes
task cluster:pause            # stop k3s and every pod
task cluster:resume           # bring it back after pause or stop
task cluster:stop             # pause, then let the distro stop so Windows gets the memory back
task cluster:delete           # lists what it would remove; `task cluster:delete -- -yes` removes it
```

Every command can be run again safely: `start` on a cluster that already matches `cluster.yaml` changes nothing and says `ok` for each piece.

| Command | Stops | Keeps | Back with |
| --- | --- | --- | --- |
| `pause` | k3s and every pod; k3s stays off across distro restarts | the node, PVCs, images, the keepalive | `resume` |
| `stop` | the above, the keepalive task, the distro | the node, PVCs, images | `resume` |
| `delete` | everything: k3s, `/etc/rancher`, `/var/lib/rancher`, the task, the kubeconfig context | the distro and the NVIDIA container toolkit | `start` |

## What `start` puts in place

- **k3s**, the release pinned in `cluster.yaml`, configured by `/etc/rancher/k3s/config.yaml`:
  - the node name;
  - `write-kubeconfig-mode: "0644"`, without which k3s rewrites its kubeconfig root-only on every start;
  - the disabled components (traefik; ingress is [#23](https://github.com/kokou-egbewatt/NeuroMesh/issues/23)).
  - It does not upgrade an installed k3s; it warns when the version differs.
- **The GPU path:**
  - **Runtime and RuntimeClass.** k3s creates them by itself when `nvidia-container-toolkit` is installed in the distro, which `start` checks but does not install.
  - **The device plugin**, from [`infra/kubernetes/gpu/`](../../infra/kubernetes/gpu/), plus a ConfigMap written from `gpu.timeSlicing`. Both go in k3s's auto-deploy folder, so they come back on every start.
- **The admission policy** ([`gpu-admission-policy.yaml`](../../infra/kubernetes/gpu/gpu-admission-policy.yaml)). Outside `kube-system`, a pod with `runtimeClassName: nvidia` must request `nvidia.com/gpu`, and a pod requesting it must use the RuntimeClass.
- **The keepalive**, a Scheduled Task named `<node> keepalive` that runs at logon, holds a hidden `wsl.exe` session on the distro, and needs no admin.
- **A Windows kubeconfig context** named after the node, pointing at `https://localhost:6443`. It is merged into `~/.kube/config` (or the first `KUBECONFIG` entry) without touching other contexts.

## A GPU pod

```yaml
spec:
  runtimeClassName: nvidia
  containers:
    - name: app
      resources:
        limits:
          nvidia.com/gpu: 1
```

Both lines, always. Each alone fails, and the policy rejects either one with a message saying what to add:

- **Without `runtimeClassName`:** the pod is scheduled and holds the GPU, reports Running, and has no driver: `libnvidia-ml.so.1: cannot open shared object file`.
- **Without the request:** the pod gets the GPU without the scheduler knowing. Anything that checks "is the GPU free" by reading allocations is then wrong.

## Why it is built this way

All of this was measured on the reference machine in [infinity-bench #22](https://github.com/kokou-egbewatt/infinity-bench/issues/22) (Windows 11, WSL2, RTX 5060, driver 621.01), which compared k3s, k3d, kind, minikube, Rancher Desktop and both Docker Desktop Kubernetes modes against a probe that must verify a GEMM on the GPU.

- **k3s in the distro**, because it is the fastest from nothing to a GPU pod (17 to 19 s, about three times faster than the rest) and the lightest (about 370 MiB), and its GPU pods run unprivileged through the runtime. The others can schedule the GPU too, contrary to what this repo used to say: minikube unprivileged as well once started with `--mount-string=/usr/lib/wsl:/usr/lib/wsl`, but about six times slower to start; kind, k3d, Rancher Desktop and Docker Desktop's kubeadm mode only with privileged pods that mount `/dev/dxg`, `/usr/lib/wsl/lib` and `/usr/lib/wsl/drivers` by hand.
- **One cluster for everything on the machine.** Every plain WSL2 distro shares one network namespace, so a second k3s in a second distro would collide on `:6443`, `:10250`, flannel and iptables. infinity-bench measures on the same cluster; before a VRAM measurement, `status` must show no pod holding `nvidia.com/gpu`.
- **The plugin needs `runtimeClassName: nvidia`.** The published manifest runs under runc, logs `Incompatible strategy detected auto`, registers nothing, and still reports Running.
- **The keepalive loops.** WSL stops a distro about a minute after its last session closes, and k3s with it. The task's own restart-on-failure is not enough: when the distro is terminated, `wsl.exe` exits 0, so the task counts it as success. The loop brings the cluster back about 9 s after `wsl --terminate`, with the GPU allocatable at 20 s.
- **The kubeconfig uses `localhost`.** WSL forwards it to the distro, so Windows `kubectl` does not depend on the node IP that changes on every WSL restart.
- **No VRAM is held by the cluster itself:** a CUDA context is 113 MiB on this card, and none of the cluster components open one.

## Reference machine

Recorded 2026-10-29 from `kubectl get node gpu-lab -o jsonpath='{.status.allocatable}'`. These figures come from the machine and its `.wslconfig`; nothing in charts or tooling may hardcode them.

| | Allocatable |
| --- | --- |
| CPU | 10 |
| Memory | 31.3 GiB (`32867928Ki`; `.wslconfig` `memory=32GB`) |
| `nvidia.com/gpu` | 1 (`gpu.timeSlicing: 1`) |
| Distro disk | see `task cluster:status` (k3s adds about 1.8 GB) |

## When something is wrong

`task cluster:status` checks the layers in order: the Windows driver, the keepalive, the distro, systemd, `/dev/dxg`, the toolkit, k3s, the containerd runtime, the node, the RuntimeClass, the device plugin, `nvidia.com/gpu`, the policy, and the kubeconfig context. Fix the first ❌; the ones after it usually follow.

Not built yet (still on #21): the local registry mirror for `task image:*` ([#14](https://github.com/kokou-egbewatt/NeuroMesh/issues/14)), the Windows relay for ports the host reaches, `.wslconfig` sizing, and the CI decision. Until the registry exists, load an image with `wsl -d Ubuntu -u root -- k3s ctr images import <file>.tar`.
