# batching benchmarks

Where vLLM stops batching well on the local GPU, and what the runtime's cap on the backend (#2) does
about it. Both scripts run as a Job in the `neuromesh` namespace, in the vLLM image.

| Command | What it does |
| --- | --- |
| `bash benchmarks/inference/batching/run.sh sweep [KEY=VALUE ...]` | `vllm bench serve` straight at vLLM, stepping concurrency, with vLLM's preemptions and KV cache use per step |
| `bash benchmarks/inference/batching/run.sh load LABEL [KEY=VALUE ...]` | Requests through the gateway's `/v1/route` to the runtime and vLLM, with the status codes, vLLM's view of each step, and the time per request at the client |

`sweep` takes `IN_LEN`, `OUT_LEN` and `CONCS`; `load` takes `MAX_TOKENS` and `CONCS`. Both need
`task vllm:up`; `load` also needs `BACKEND=vllm task svc:up`, with `MAX_IN_FLIGHT` and `MAX_QUEUE`
for the cap. Results are one JSON line per step, kept in `results/<date>/`.

## 2026-11-20

RTX 5060 8 GB under WSL2, k3s, `vllm/vllm-openai:v0.12.0` with `--enforce-eager`,
Qwen2.5-0.5B-Instruct at `gpuMemoryUtilization` 0.5: a KV cache of 142,032 tokens. The desktop held
3.3 GB of the card. One runtime replica took every request (the gateway picks the first, #3).

### Straight at vLLM

128 tokens in and 1024 out, so each request ends holding 1,152 tokens of KV cache.
[`vllm-sweep.jsonl`](results/2026-11-20/vllm-sweep.jsonl).

| Concurrent | Preemptions | Peak KV | Output tok/s | TTFT p99 | Time per token p99 | Request p50 |
| --- | --- | --- | --- | --- | --- | --- |
| 16 | 0 | 13% | 1,153 | 99 ms | 14.0 ms | 14.2 s |
| 32 | 0 | 26% | 2,332 | 145 ms | 14.4 ms | 14.0 s |
| 64 | 0 | 52% | 5,062 | 241 ms | 12.8 ms | 12.9 s |
| 96 | 0 | 77% | 6,984 | 305 ms | 13.9 ms | 14.0 s |
| 128 | 10 | 100% | 8,364 | 442 ms | 15.0 ms | 15.2 s |
| 160 | 74 | 100% | 7,545 | 1,221 ms | 22.2 ms | 19.2 s |
| 192 | 138 | 100% | 7,660 | 2,079 ms | 27.9 ms | 22.0 s |
| 256 | 552 | 100% | 7,887 | 6,017 ms | 40.4 ms | 30.2 s |

vLLM batches everything in flight, and throughput rises almost linearly until the cache is full, at
142,032 / 1,152 ≈ 123 requests. Past that it preempts and queues without limit: throughput stays flat
and every request gets slower.

### Through the gateway, with and without the cap

528 tokens in and up to 512 out (377 on average), so the sizing rule gives 142,032 / (528 + 512) ≈ 136;
the cap is 128. The gateway gives a request 30 s. [`gateway-cap.jsonl`](results/2026-11-20/gateway-cap.jsonl).

| Cap | Concurrent | OK | 504 | 429 | Preemptions | Useful tok/s | Time per token (mean) | TTFT in vLLM (mean) | Request p99 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| off | 128 | 256 | 0 | 0 | 0 | 2,692 | 40 ms | 0.33 s | 24.3 s |
| off | 192 | 317 | 67 | 0 | 25 | 2,270 | 57 ms | 1.03 s | 29.8 s |
| off | 256 | 246 | 266 | 0 | 163 | 1,321 | 70 ms | 3.71 s | 30.1 s |
| 128, queue 128 | 192 | 333 | 51 | 0 | 0 | 2,329 | 42 ms | 0.52 s | 29.8 s |
| 128, queue 128 | 256 | 272 | 240 | 0 | 0 | 1,575 | 44 ms | 0.56 s | 29.9 s |
| 128, queue 16 | 192 | 144 | 0 | 240 | 0 | 2,387 | 37 ms | 1.02 s | 23.4 s |
| 128, queue 16 | 256 | 144 | 0 | 368 | 0 | 2,518 | 36 ms | 0.48 s | 21.2 s |
| 128, queue 16 (again) | 256 | 144 | 0 | 368 | 0 | 2,594 | 36 ms | 0.34 s | 22.1 s |

Without the cap, half the requests at 256 ran out the gateway's deadline after using the GPU, and
useful throughput halved. The cap stops preemption at any queue size, but a queue holding a full turn
of the cap (128) still runs requests past the deadline: they wait about one answer's time for a slot,
then take another. A short queue turns that wait into a 429 `backend_saturated` at once, and what is
admitted keeps the throughput and the time per token of the uncapped 128 run.

So size the cap from the KV cache, and the queue from the deadline: a queued request waits about
(queue / cap) answers, and that plus its own answer has to fit in the gateway's `upstream_timeout`.

The load generator sends the next request as soon as one returns, so a 429 comes back and is replaced
at once; real clients back off, and would see fewer of them.
