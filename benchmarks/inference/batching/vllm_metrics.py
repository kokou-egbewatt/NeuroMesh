"""Reads vLLM's /metrics: counters, gauges, and the histograms the reports summarize."""
import re
import urllib.request

HISTS = {
    "tpot": "request_time_per_output_token_seconds",
    "ttft": "time_to_first_token_seconds",
    "vllm_queue": "request_queue_time_seconds",
}
VALUES = ("num_preemptions_total", "num_requests_running", "num_requests_waiting", "kv_cache_usage_perc")


def scrape(url):
    text = urllib.request.urlopen(url, timeout=5).read().decode()
    out = {}
    for name in VALUES:
        m = re.search(r"^vllm:%s\{[^}]*\} ([0-9.e+-]+)$" % name, text, re.M)
        out[name] = float(m.group(1)) if m else 0.0
    for key, name in HISTS.items():
        h = {"buckets": {}}
        for suffix in ("sum", "count"):
            m = re.search(r"^vllm:%s_%s\{[^}]*\} ([0-9.e+-]+)$" % (name, suffix), text, re.M)
            h[suffix] = float(m.group(1)) if m else 0.0
        for le, v in re.findall(r'^vllm:%s_bucket\{[^}]*le="([^"]+)"[^}]*\} ([0-9.e+-]+)$' % name, text, re.M):
            h["buckets"][le] = float(v)
        out[key] = h
    return out


def summary(before, after):
    """Mean and the p99 bucket bound of each histogram between two scrapes, and the preemptions."""
    out = {"preemptions": after["num_preemptions_total"] - before["num_preemptions_total"]}
    for key in HISTS:
        b, a = before[key], after[key]
        cnt = a["count"] - b["count"]
        out[key + "_mean_ms"] = round((a["sum"] - b["sum"]) / cnt * 1000, 1) if cnt else None
        p99 = None
        for le in sorted(a["buckets"], key=lambda s: float("inf") if s == "+Inf" else float(s)):
            if cnt and a["buckets"][le] - b["buckets"].get(le, 0) >= 0.99 * cnt:
                p99 = le
                break
        out[key + "_p99_le_s"] = p99
    return out


class Peaks:
    """Highest gauge values seen while sampling."""

    def __init__(self):
        self.v = {}

    def add(self, m):
        for k in ("num_requests_running", "num_requests_waiting", "kv_cache_usage_perc"):
            self.v[k] = max(self.v.get(k, 0), m[k])

    def report(self):
        return {"vllm_peak_running": self.v.get("num_requests_running"),
                "vllm_peak_waiting": self.v.get("num_requests_waiting"),
                "vllm_peak_kv": round(self.v.get("kv_cache_usage_perc", 0), 3)}
