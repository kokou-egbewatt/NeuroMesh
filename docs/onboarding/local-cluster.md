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

One exception: a **GPU observer**, which reads counters around the clock and computes nothing, may use the RuntimeClass without the request, so it doesn't hold the one `nvidia.com/gpu` workloads need. It must be in the `observability` namespace, carry `neuromesh.dev/gpu-observer: "true"`, be owned by a DaemonSet, and set `NVIDIA_VISIBLE_DEVICES=all`. dcgm-exporter is the only one. `task cluster:status -- -probe` checks that the exception holds and that a bare pod with the label, or the label elsewhere, is still rejected. Measured by difference, dcgm-exporter's VRAM is below the desktop's noise (medians 6969 MiB running, 6985 parked, 7032 running again, 30 to 35 MiB spread): it opens no CUDA context.

## Why it is built this way

All of this was measured on the reference machine in [infinity-bench #22](https://github.com/kokou-egbewatt/infinity-bench/issues/22) (Windows 11, WSL2, RTX 5060, driver 621.01), which compared k3s, k3d, kind, minikube, Rancher Desktop and both Docker Desktop Kubernetes modes against a probe that must verify a GEMM on the GPU.

- **k3s in the distro**, because it is the fastest from nothing to a GPU pod (17 to 19 s, about three times faster than the rest) and the lightest (about 370 MiB), and its GPU pods run unprivileged through the runtime. The others can schedule the GPU too, contrary to what this repo used to say: minikube unprivileged as well once started with `--mount-string=/usr/lib/wsl:/usr/lib/wsl`, but about six times slower to start; kind, k3d, Rancher Desktop and Docker Desktop's kubeadm mode only with privileged pods that mount `/dev/dxg`, `/usr/lib/wsl/lib` and `/usr/lib/wsl/drivers` by hand.
- **One cluster for everything on the machine.** Every plain WSL2 distro shares one network namespace, so a second k3s in a second distro would collide on `:6443`, `:10250`, flannel and iptables. infinity-bench measures on the same cluster; before a VRAM measurement, `status` must show no pod holding `nvidia.com/gpu`.
- **The plugin needs `runtimeClassName: nvidia`.** The published manifest runs under runc, logs `Incompatible strategy detected auto`, registers nothing, and still reports Running.
- **The keepalive loops.** WSL stops a distro about a minute after its last session closes, and k3s with it. The task's own restart-on-failure is not enough: when the distro is terminated, `wsl.exe` exits 0, so the task counts it as success. The loop brings the cluster back about 9 s after `wsl --terminate`, with the GPU allocatable at 20 s.
- **The kubeconfig uses `localhost`.** WSL forwards it to the distro, so Windows `kubectl` does not depend on the node IP that changes on every WSL restart.
- **No VRAM is held by the cluster itself:** a CUDA context is 113 MiB on this card, and none of the cluster components open one.

## Local hostnames

Every UI on the cluster is `https://<name>.neuromesh.localhost`, from a browser or curl on Windows, with no port-forward, relay or hosts file. Issues: [#21](https://github.com/kokou-egbewatt/NeuroMesh/issues/21) (the relay it turned out not to need) and [#23](https://github.com/kokou-egbewatt/NeuroMesh/issues/23) (ingress: Envoy Gateway).

```sh
task certs:dev -- -force   # once: mint the dev CA again, now with the edge certificate
task certs:trust           # once per new CA: trust it for this Windows user (a Windows prompt)
task edge:up               # Envoy Gateway, the Gateway on :443, and the profile's routes
task edge:status           # every routed hostname over HTTPS from this machine
task obs:access            # the URLs and logins
```

- **Names.** `*.localhost` resolves to loopback by standard (RFC 6761) in browsers and curl, so a new UI needs a route, not a DNS entry. Windows' own resolver doesn't follow that rule, so PowerShell's `Invoke-WebRequest` can't reach these names; browsers and curl can.
- **The path.** Envoy runs on the node's network and binds `:443` itself, so its listener is a real socket in the distro, and WSL2's localhost forwarding carries `127.0.0.1:443` on Windows to it: the same path the kubeconfig's `localhost:6443` takes. A LoadBalancer Service through servicelb would be iptables rules, which the forwarding does not see, and is why the other route to these UIs would have needed a relay process.
- **Root.** The proxy runs as root with `NET_BIND_SERVICE` added back, because Envoy Gateway drops every capability and a non-root process can't bind 443. It replaces itself on upgrade instead of rolling, since two proxies on the node's network collide on their ports.
- **TLS.** One listener for `*.neuromesh.localhost`, with the `edge` certificate from the dev CA. The CA lives 90 days; when it is minted again, run `task certs:trust` again. Windows curl needs `--ssl-no-revoke` with it, because schannel asks for a revocation list a dev CA doesn't publish; browsers treat that as soft.
- **After a restart** it comes back on its own. Measured with `wsl --terminate Ubuntu`: the keepalive restarts the distro, Envoy binds `:443` again after about a minute, and every hostname answered within about 3 minutes, Grafana last. A `FAIL` from `task edge:status` in the first minutes is the backends still starting.
- **Port 443 on Windows** must be free. Anything else bound to `127.0.0.1:443` takes the traffic instead, and `task edge:status` reports every name as unreachable.
- **A chart opts in** with an HTTPRoute whose parent is the Gateway `edge/neuromesh-edge`, section `https`, in a namespace labelled `neuromesh.dev/edge: "true"`. Render it only when `.Capabilities.APIVersions.Has "gateway.networking.k8s.io/v1/HTTPRoute"`, so the chart still installs without the edge, as `infra/helm/victoria-metrics/templates/routes.yaml` does.

## Reference machine

Recorded 2026-10-29 from `kubectl get node gpu-lab -o jsonpath='{.status.allocatable}'`. These figures come from the machine and its `.wslconfig`; nothing in charts or tooling may hardcode them.

| | Allocatable |
| --- | --- |
| CPU | 10 |
| Memory | 31.3 GiB (`32867928Ki`; `.wslconfig` `memory=32GB`) |
| `nvidia.com/gpu` | 1 (`gpu.timeSlicing: 1`) |
| Distro disk | see `task cluster:status` (k3s adds about 1.8 GB) |

### The observability profile

`task obs:up` installs it (#22; for now the metrics half). Measured 2026-11-16 with `kubectl top` a few minutes after install, idle, at Phase 1 scrape volume:

| | Requests | In use |
| --- | --- | --- |
| CPU, whole profile | 380m (3.8% of 10) | about 100m |
| Memory, whole profile | 1099 MiB (3.4% of 31.3 GiB) | about 1182 MiB |
| VMSingle | 100m, 256 MiB | 39m, 324 MiB |
| Ingest | | 3392 samples/s, 11,234 series (kubelet and apiserver are 85% of it; DCGM about 55 per scrape) |

VMSingle keeps 45 days (`retentionPeriod: 45d`). It has no size-based retention, so the bound by bytes is `-storage.minFreeDiskSpaceBytes=5GiB`: below that much free space on the distro it stops ingesting instead of filling the disk, which matters because k3s's local-path storage does not enforce the 10Gi PVC. Even at a pessimistic 1 byte per sample, 45 days at this rate is under 25 GiB, against 943 GB free on the reference distro.

## When something is wrong

`task cluster:status` checks the layers in order: the Windows driver, the keepalive, the distro, systemd, `/dev/dxg`, the toolkit, k3s, the containerd runtime, the node, the RuntimeClass, the device plugin, `nvidia.com/gpu`, the policy, and the kubeconfig context. Fix the first ❌; the ones after it usually follow.

Not built yet (still on #21): the local registry mirror for `task image:*` ([#14](https://github.com/kokou-egbewatt/NeuroMesh/issues/14)), the Windows relay for ports the host reaches, `.wslconfig` sizing, and the CI decision. Until the registry exists, load an image with `wsl -d Ubuntu -u root -- k3s ctr images import <file>.tar`.
