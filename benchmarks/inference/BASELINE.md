# Stub-path baseline

This file records the latency and throughput of the stub path, gateway to runtime, before any real
backend or hot-path middleware exists. #1 (real backend), #6 (auth), #7 (rate limiting) and #10
(tracing, capped at 5% overhead against this file) compare against it. It is a regression fixture:
a change that moves these numbers says so in its PR, with a new run beside the old one.

## What is measured

`task bench` boots the runtime and the gateway in-process through the integration harness
(`test/integration/harness`), the same wiring the golden tests use:

- **route**: `POST /v1/route` over HTTPS (TLS 1.3) to the gateway, which validates at the edge,
  calls `Route` on the runtime over mutual TLS, and answers. The client keeps one warm connection
  per worker, so handshakes are not in the numbers.
- **stream**: `StreamInference` on the runtime through the Go SDK over mutual TLS, presenting the
  gateway's certificate. The gateway has no streaming endpoint until #4. The stub sends one token
  chunk and one usage chunk, so chunks/s is two per stream until #1 produces real tokens.
- **handler**: `BenchmarkRouteHandler` runs the gateway's HTTP handler alone against a fake client,
  with no sockets and no TLS. The difference between it and the route row is the cost of the wire.

The driver is closed loop: each of N workers sends its share of a fixed request count back to back,
after a discarded warmup, and keeps every latency. Percentiles are exact (nearest rank over the
sorted samples). A slow response delays that worker's next request rather than queueing behind it,
so the percentiles are service time at that concurrency, not what an open-loop client at a fixed
rate would see. For comparing overhead before and after a change, that is the measurement wanted.
Any failed request fails the run.

Every worker records at least 1000 requests, so the c=64 case records 64000. With fewer, it ran
for under half a second and a single burst of background work on the host moved its p50 by more
than 20% between sessions.

The client, the gateway and the runtime share one process and one machine, so the c=64 rows
saturate the CPU. They measure the stack under contention, and they are the first rows to move
when anything else runs on the host.

The services log at info in JSON, as deployed, into `io.Discard`: the formatting cost stays in the
path, the terminal does not.

## Protocol

1. Run on a quiet machine on AC power: close browsers, IDE indexing, builds and anything else that
   uses CPU.
2. `task bench:docker` runs the matrix twice in a Linux container and fails if any p50 moves by 10%
   or more between the two runs. Record only a run that passes.
3. Paste the second run's metadata and table below. Replace the reference only for a deliberate
   rebaseline; otherwise add the new run under it and compare.
4. `task bench` and `task bench:repeat` do the same natively. Use them for quick local comparisons,
   never against the reference table.

Compare runs only within one environment. The same machine gives very different numbers natively
on Windows and in a Linux container (twice the p50 in the container at low concurrency). The
container is the reference because the deployment target is Linux (k3s in WSL2, #21).

## Reference: in-process, Linux container

- Date: 2026-09-28
- Commit: `143622b-dirty` (the #13 working tree; the stub path is unchanged from `143622b`)
- Go: go1.26.8 linux/amd64, `golang:1.26.8` under Docker Desktop (WSL2)
- CPU: AMD Ryzen 5 8400F 6-Core Processor (10 logical visible to the container, GOMAXPROCS 10)
- Requests per case: 20000 recorded (at least 1000 per worker) after 2000 warmup
- Repeatability: second run against the first, every p50 within 1.3%

| Scenario | Concurrency | Requests | p50 (µs) | p95 (µs) | p99 (µs) | max (µs) | req/s | first chunk p50 / p99 (µs) | chunks/s |
| --- | --: | --: | --: | --: | --: | --: | --: | --: | --: |
| route | 1 | 20000 | 343.7 | 484.3 | 895.6 | 4329.9 | 2820 | n/a | n/a |
| route | 8 | 20000 | 449.0 | 1228.4 | 1561.9 | 2866.0 | 14364 | n/a | n/a |
| route | 64 | 64000 | 1038.4 | 3165.4 | 3933.0 | 9296.3 | 44780 | n/a | n/a |
| stream | 1 | 20000 | 186.2 | 255.5 | 504.0 | 1159.6 | 5109 | 178.6 / 468.9 | 10217 |
| stream | 8 | 20000 | 256.4 | 625.1 | 871.9 | 2044.7 | 26772 | 249.6 / 847.4 | 53544 |

Five further c=64 runs on their own (`go test -bench 'BenchmarkRoute/c=64$' -benchtime 30000x
-count 5`, same container) gave a p50 between 1026 and 1076 µs, which agrees with the row above.

Gateway handler alone (`go test -bench RouteHandler -benchmem -count 5`, same container):

| Benchmark | ns/op (5 runs) | B/op | allocs/op |
| --- | --: | --: | --: |
| RouteHandler | 6808 to 6919 | 8885 | 50 |

The handler is about 7 µs of the 344 µs route p50, about 2%. The rest is the HTTPS edge, the mutual
TLS gRPC hop and the runtime, which is where #10's interceptors and #1's backend call will land.

## Native Windows, same machine (not the reference)

- Date: 2026-09-28, commit `143622b-dirty`, go1.26.8 windows/amd64, 12 logical CPUs
- Repeatability: every p50 within 6.0%

| Scenario | Concurrency | Requests | p50 (µs) | p95 (µs) | p99 (µs) | req/s | first chunk p50 / p99 (µs) |
| --- | --: | --: | --: | --: | --: | --: | --: |
| route | 1 | 20000 | 177.1 | 331.2 | 556.9 | 4827 | n/a |
| route | 8 | 20000 | 371.5 | 902.8 | 1210.6 | 17887 | n/a |
| route | 64 | 64000 | 992.8 | 2585.5 | 3275.4 | 51393 | n/a |
| stream | 1 | 20000 | 87.7 | 187.3 | 12531.0 | 3069 | 81.4 / 12483.3 |
| stream | 8 | 20000 | 142.2 | 465.0 | 7230.9 | 24614 | 136.2 / 7212.6 |

RouteHandler natively: 6483 to 7007 ns/op, 8891 B/op, 50 allocs/op.

Three things make this table unfit as the reference:

- **Stream p99 of 7 to 13 ms.** Roughly 1% of streams stall for about 10 ms on Windows and never in
  the container, which is why stream req/s is lower natively despite the lower p50. It comes from
  the Windows network stack or scheduler, not from NeuroMesh.
- **Drift between sessions.** Route c=8 read 230 µs in an earlier session and 371 µs in this one,
  each session passing its own repeatability check, and one native `bench:repeat` failed at +47%.
- **The clock.** Go's monotonic clock on Windows advances only about every 0.5 ms, so `time.Since`
  reads 0 for most of these requests. The driver reads QueryPerformanceCounter (100 ns) on Windows
  instead and refuses to run on any clock coarser than 1 µs (`driver/clock*.go`).

## Cluster (recorded in #14)

The services as deployed by their Helm charts on the local cluster, measured with the same driver
and matrix from a Job inside the cluster: `task bench:cluster` after `task svc:up`.

- Date: 2026-11-16
- Commit: `8063063` (images `neuromesh-runtime` and `neuromesh-gateway` tagged `8063063af144`)
- Node: `gpu-lab`, k3s v1.36.5+k3s1, Ubuntu 24.04.1 in WSL2, kernel 6.6.114.1-microsoft-standard-WSL2,
  containerd 2.3.4-k3s1.36; allocatable 10 CPU and 31.3 GiB (`.wslconfig`), on the Ryzen 5 8400F
- Charts: gateway 1 replica, runtime 2 replicas; each requests 50m CPU and 32Mi, limits memory to
  128Mi, no CPU limit
- Driver: `neuromesh-bench`, a Job in the `neuromesh` namespace (go1.26.8 linux/amd64, GOMAXPROCS
  10), calling `https://gateway:8443`, the gateway's ClusterIP Service, over TLS 1.3 verified
  against the dev CA. The gateway calls the runtime through its headless Service over mutual TLS.
  The driver shares the node's CPUs with the services, as it shares them in-process.
- Requests per case: 20000 recorded (at least 1000 per worker) after 2000 warmup
- Repeatability: second run against the first, every p50 within 6.6%

| Scenario | Concurrency | Requests | p50 (µs) | p95 (µs) | p99 (µs) | max (µs) | req/s | first chunk p50 / p99 (µs) | chunks/s |
| --- | --: | --: | --: | --: | --: | --: | --: | --: | --: |
| route | 1 | 20000 | 623.5 | 799.1 | 1044.8 | 9469.7 | 1547 | n/a | n/a |
| route | 8 | 20000 | 961.3 | 1654.4 | 2177.8 | 5182.9 | 7762 | n/a | n/a |
| route | 64 | 64000 | 2989.6 | 6247.0 | 8302.9 | 22075.7 | 19440 | n/a | n/a |

No stream rows: the stream case calls the runtime directly, which isn't exposed outside its pods,
and the gateway has no streaming endpoint until #4.

Against the in-process reference, the cluster costs 1.8 to 2.9 times the p50, for the hop from the
driver's pod to the gateway's and from the gateway to a runtime pod, each through the cluster
network instead of loopback.

How much the desktop is doing moves these more than anything in the services. The same commit gave
a c=64 p50 of 3.0 ms with the machine quiet and 5.1 to 5.6 ms in four earlier sessions with the
desktop busy, two of which failed the 10% check. Four passes in one session, with the gateway and
runtime restarted after the second, stayed within 10% of each other, so the services don't slow down
as they run. Record only on a quiet machine, as the protocol above says.

Two other ways in, not the reference:

- **From Windows through the local edge** (`bench -target https://gateway.neuromesh.localhost`),
  every request crosses WSL's localhost relay and the edge's Envoy as well. It did not repeat:
  c=8 drifted 77% and c=64 38% between two runs, and Envoy used up to 2.4 cores. It measures the
  relay more than the services.
- **One runtime takes the traffic.** The gateway dials the headless Service with gRPC's default
  pick-first policy, so it keeps one connection to one runtime pod; during a run one pod's memory
  grew while the other's stayed flat. Spreading load across replicas is #3.

## Instrumentation overhead (recorded in #10)

What tracing and metrics add to the stub path, measured in the reference container with one build of
the driver, instrumentation off against `bench -instrumented` (every span built, batched and
dropped by a no-op exporter; metrics on in both services). Two rounds each, alternated, on
2026-11-16; p50 change against off, averaged:

| p50 | metrics only | tracing 10% + metrics | tracing 100% + metrics |
| --- | --: | --: | --: |
| route c=1 | +1% | 0% | +6% |
| route c=8 | -1% | -11% (noise) | +4% |
| route c=64 | +4% | +0.3% | +27% |
| stream c=1 | -1% | -2% | -2% |
| stream c=8 | -2% | -2% | +2% |

Metrics cost nothing measurable. Tracing every request does, under concurrency: three spans per
request through one batching queue at about 40,000 requests a second. The services therefore keep
10% of new traces (`OTEL_TRACES_SAMPLER_ARG`), within the 5% budget everywhere, and always follow a
client's sampled `traceparent`, which is how the cluster test traces its own request.

An earlier comparison of `main` against this branch, two rounds each, moved more between two
runs of the same build (route c=64: 1307 and 2326 µs) than between the builds, which is why the
numbers above come from one binary.
