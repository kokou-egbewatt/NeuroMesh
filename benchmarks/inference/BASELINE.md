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

The cluster row is part of [#14](https://github.com/kokou-egbewatt/NeuroMesh/issues/14), the first
change that runs the services on the local cluster through Helm charts. It adds the node spec
(`kubectl get node -o wide` and `kubectl describe node`), both charts' resource requests and
limits, how the driver reaches the gateway, and the commit. The same driver and matrix apply; only
the target changes.
