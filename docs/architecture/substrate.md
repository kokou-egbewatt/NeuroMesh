# Substrate

The machine NeuroMesh runs on locally: Windows, one WSL2 distro, k3s, and one GPU. How to run it is in [Local cluster](../onboarding/local-cluster.md); this page is the picture.

## Layers

`task cluster:status` walks these from the bottom up, and the first one that fails is the one to fix.

```mermaid
flowchart BT
    driver["Windows NVIDIA driver"]
    keep["keepalive Scheduled Task<br/>holds a wsl.exe session"]
    distro["WSL2 Ubuntu distro<br/>systemd, /dev/dxg"]
    toolkit["nvidia-container-toolkit"]
    k3s["k3s<br/>containerd, nvidia runtime"]
    node["node gpu-lab"]
    rc["RuntimeClass nvidia"]
    plugin["device plugin<br/>advertises nvidia.com/gpu: 1"]
    policy["admission policy<br/>RuntimeClass and request together"]
    pods["GPU pods"]

    driver --> distro
    keep -->|"stops WSL idling it out"| distro
    distro --> toolkit --> k3s --> node
    node --> rc --> plugin --> policy --> pods
```

## From Windows

Nothing is port-forwarded. WSL2 forwards Windows `localhost` to sockets bound in the distro, so two ports are enough.

```mermaid
flowchart LR
    subgraph windows["Windows"]
        kubectl["kubectl, helm<br/>context gpu-lab"]
        browser["browser, curl"]
        docker["Docker"]
    end

    subgraph distro["WSL2 distro"]
        api["k3s API :6443"]
        envoy["Envoy :443<br/>host network"]
        ctr["containerd"]
    end

    kubectl -->|"localhost:6443"| api
    browser -->|"*.neuromesh.localhost:443"| envoy
    docker -->|"docker save, k3s ctr import"| ctr
```

- **Hostnames.** `*.localhost` resolves to loopback in browsers and curl, so a new UI needs an HTTPRoute, not a DNS entry.
- **Images.** No registry: images are imported into containerd, tagged with the commit, and pulled with `pullPolicy: Never`.

## Namespaces

```mermaid
flowchart TB
    subgraph ks["kube-system"]
        dp["NVIDIA device plugin"]
    end
    subgraph egs["envoy-gateway-system"]
        eg["Envoy Gateway controller"]
    end
    subgraph edge["edge"]
        gwy["Gateway neuromesh-edge<br/>edge-tls"]
    end
    subgraph nm["neuromesh (Pod Security restricted)"]
        gw["gateway"]
        rt["runtime x2"]
        ol["Ollama or vLLM, optional"]
    end
    subgraph obs["observability"]
        stack["VictoriaMetrics stack, VictoriaLogs,<br/>Tempo, OTel collector, dcgm-exporter"]
    end

    eg -->|"programs"| gwy
    gwy -->|"HTTPRoutes"| gw & ol & stack
```

## The GPU

The node advertises one `nvidia.com/gpu`. Whatever requests it holds the card, and nothing else that needs it schedules.

| Holder | Requests the GPU | How |
| --- | --- | --- |
| Ollama | yes | `runtimeClassName: nvidia` plus `nvidia.com/gpu: 1`; `task ollama:down` frees it |
| vLLM | yes | the same; `task vllm:down` frees it. It also takes a fixed share of the card's memory at start (`gpuMemoryUtilization`) |
| infinity-bench runs | yes | same cluster; need the GPU free first |
| dcgm-exporter | no | the GPU observer exception: reads counters, opens no CUDA context |

The admission policy rejects a pod with one of the two lines and not the other, except the observer.
