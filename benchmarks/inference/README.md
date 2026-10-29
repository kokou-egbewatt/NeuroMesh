# inference benchmarks

Latency and throughput of the inference path, gateway to runtime. The recorded numbers and the
protocol for recording new ones are in [BASELINE.md](BASELINE.md).

| Command | What it does |
| --- | --- |
| `task bench:docker` | The reference run: the matrix twice in a Linux container, failing on a p50 drift of 10% or more |
| `task bench` | One native run plus the gateway handler micro-benchmark, results in `results/latest.json` |
| `task bench:repeat` | Two native runs and the same drift check |
| `go test -bench . ./benchmarks/inference/` | The same cases as `testing.B` benchmarks, for benchstat |

The layout:

- `driver/` is the closed-loop load driver: fixed request counts, warmup, exact percentiles, and a
  clock precise enough for sub-millisecond requests on every OS.
- `scenarios.go` defines what one request is (`route`, `stream`), and `report.go` defines the
  matrix, the table and the drift comparison.
- `cmd/bench` boots the stack through `test/integration/harness` and runs the matrix.
