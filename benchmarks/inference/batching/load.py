"""The runtime's cap under load: requests through the gateway's /v1/route, stepping concurrency (#2).

Every prompt starts with its own request number, so vLLM's prefix cache can't share their KV blocks.
The API has no ignore_eos, so answers end at MAX_TOKENS or earlier; the report gives the mean.
"""
import asyncio
import json
import os
import ssl
import time

import aiohttp

from vllm_metrics import Peaks, scrape, summary

ROUTE = os.environ.get("TARGET", "https://gateway:8443") + "/v1/route"
METRICS = os.environ.get("VLLM", "http://vllm:8000") + "/metrics"
MODEL = os.environ.get("MODEL", "Qwen/Qwen2.5-0.5B-Instruct")
MAX_TOKENS = int(os.environ.get("MAX_TOKENS", "512"))
CONCS = [int(c) for c in os.environ.get("CONCS", "128,192,256").split(",")]
LABEL = os.environ.get("LABEL", "")
FILLER = ("The lighthouse stood at the end of a long stone pier, and every night its keeper climbed the "
          "spiral stairs to light the lamp. Ships passed in the dark, their captains trusting the beam to "
          "show them the rocks. ") * 11


async def sample(stop, peaks):
    while not stop.is_set():
        try:
            peaks.add(await asyncio.to_thread(scrape, METRICS))
        except OSError:
            pass
        await asyncio.sleep(0.5)


async def one(session, i):
    body = {"model": MODEL, "prompt": "Request %d. %sContinue this story for as long as you can." % (i, FILLER),
            "max_tokens": MAX_TOKENS, "temperature": 0.01}
    t = time.monotonic()
    try:
        async with session.post(ROUTE, json=body) as r:
            data = await r.json(content_type=None)
            return {"status": r.status, "s": time.monotonic() - t, "code": (data.get("error") or {}).get("code"),
                    "prompt": data.get("prompt_tokens", 0), "completion": data.get("completion_tokens", 0)}
    except (aiohttp.ClientError, asyncio.TimeoutError) as e:
        return {"status": 0, "s": time.monotonic() - t, "code": type(e).__name__, "prompt": 0, "completion": 0}


async def run(c):
    n = max(2 * c, 64)
    tls = ssl.create_default_context(cafile=os.environ.get("CA", "/ca/ca.crt"))
    sem = asyncio.Semaphore(c)
    before = scrape(METRICS)
    stop, peaks = asyncio.Event(), Peaks()
    sampler = asyncio.create_task(sample(stop, peaks))

    async def bounded(session, i):
        async with sem:
            return await one(session, i)

    t = time.monotonic()
    async with aiohttp.ClientSession(connector=aiohttp.TCPConnector(ssl=tls, limit=c),
                                     timeout=aiohttp.ClientTimeout(total=120)) as s:
        res = await asyncio.gather(*(bounded(s, i) for i in range(n)))
    wall = time.monotonic() - t
    stop.set()
    await sampler

    ok = [r for r in res if r["status"] == 200]
    lat = sorted(r["s"] for r in ok)
    codes = {}
    for r in res:
        k = str(r["status"]) + (":" + r["code"] if r["code"] else "")
        codes[k] = codes.get(k, 0) + 1
    toks = sum(r["completion"] for r in ok)
    print(json.dumps({
        "label": LABEL, "concurrency": c, "requests": n, "codes": codes,
        **summary(before, scrape(METRICS)), **peaks.report(),
        "out_tok_s": round(toks / wall, 1),
        "mean_prompt_tokens": round(sum(r["prompt"] for r in ok) / max(len(ok), 1), 1),
        "mean_completion_tokens": round(toks / max(len(ok), 1), 1),
        "e2e_p50_s": round(lat[len(lat) // 2], 2) if lat else None,
        "e2e_p99_s": round(lat[min(len(lat) - 1, len(lat) * 99 // 100)], 2) if lat else None,
        "wall_s": round(wall, 1),
    }), flush=True)


async def main():
    for c in CONCS:
        await run(c)


asyncio.run(main())
