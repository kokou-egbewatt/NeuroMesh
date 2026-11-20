"""Where vLLM starts preempting: `vllm bench serve` straight at vLLM, stepping concurrency (#2).

Fixed lengths with ignore_eos, so every request holds IN_LEN + OUT_LEN tokens of KV cache at its end.
"""
import json
import os
import subprocess
import threading
import time

from vllm_metrics import Peaks, scrape, summary

BASE = os.environ.get("VLLM", "http://vllm:8000")
MODEL = os.environ.get("MODEL", "Qwen/Qwen2.5-0.5B-Instruct")
IN_LEN = int(os.environ.get("IN_LEN", "128"))
OUT_LEN = int(os.environ.get("OUT_LEN", "1024"))
CONCS = [int(c) for c in os.environ.get("CONCS", "16,32,64,96,128,160,192,256").split(",")]


def sample(stop, peaks):
    while not stop.is_set():
        try:
            peaks.add(scrape(BASE + "/metrics"))
        except OSError:
            pass
        time.sleep(0.5)


for c in CONCS:
    n = max(2 * c, 32)
    before = scrape(BASE + "/metrics")
    stop, peaks = threading.Event(), Peaks()
    t = threading.Thread(target=sample, args=(stop, peaks), daemon=True)
    t.start()
    r = subprocess.run([
        "vllm", "bench", "serve", "--backend", "openai", "--endpoint", "/v1/completions",
        "--base-url", BASE, "--model", MODEL, "--tokenizer", "/data/models/" + MODEL,
        "--dataset-name", "random", "--random-input-len", str(IN_LEN), "--random-output-len", str(OUT_LEN),
        "--ignore-eos", "--num-prompts", str(n), "--max-concurrency", str(c),
        "--percentile-metrics", "ttft,tpot,e2el", "--metric-percentiles", "50,99",
        "--save-result", "--result-dir", "/tmp/out", "--result-filename", "c%d.json" % c,
    ], capture_output=True, text=True)
    stop.set()
    t.join()
    if r.returncode != 0:
        print(json.dumps({"concurrency": c, "error": r.stderr[-2000:]}), flush=True)
        continue
    res = json.load(open("/tmp/out/c%d.json" % c))
    print(json.dumps({
        "concurrency": c, "prompts": n, "in": IN_LEN, "out": OUT_LEN,
        **summary(before, scrape(BASE + "/metrics")), **peaks.report(),
        "out_tok_s": round(res["output_throughput"], 1),
        "ttft_p50_ms": round(res["p50_ttft_ms"], 1), "ttft_p99_ms": round(res["p99_ttft_ms"], 1),
        "tpot_p50_ms": round(res["p50_tpot_ms"], 2), "tpot_p99_ms": round(res["p99_tpot_ms"], 2),
        "e2el_p50_ms": round(res["p50_e2el_ms"], 1), "failed": n - res["completed"],
    }), flush=True)
