#!/usr/bin/env python3
"""analyze.py — compute the evaluation rubric metrics for one experiment
mode directory (samples.csv, summary.json, pods/events finals) and print
a Markdown-ready block. Usage: analyze.py <data-dir> <label>."""

import csv
import json
import os
import sys

def load(mode_dir):
    d = {}
    with open(os.path.join(mode_dir, "samples.csv")) as f:
        d["samples"] = list(csv.DictReader(f))
    with open(os.path.join(mode_dir, "summary.json")) as f:
        d["summary"] = json.load(f)
    with open(os.path.join(mode_dir, "persecond.jsonl")) as f:
        d["persec"] = [json.loads(l) for l in f if l.strip()]
    for name in ("pods-final.json", "events-final.json"):
        p = os.path.join(mode_dir, name)
        if os.path.exists(p):
            with open(p) as f:
                d[name.split(".")[0]] = json.load(f)
    return d

def main(mode_dir, label):
    d = load(mode_dir)
    s = d["summary"]
    samples = d["samples"]
    t0 = int(samples[0]["time"])

    # replica timeline stats
    specs = [int(r["spec_replicas"]) for r in samples]
    peak_start, peak_end = 120, 300  # sustained 40 rps window (measured)
    # samples begin when the sampler starts (~load start); approximate by
    # relative index: sampler ran 10 s ticks over ~720 s
    peak_rows = [r for r in samples if peak_start <= int(r["time"]) - t0 < peak_end]
    peak_specs = [int(r["spec_replicas"]) for r in peak_rows] or specs
    peak_cpu = [float(r["cpu_milli"]) for r in peak_rows]
    peak_pct = [float(r["cpu_pct_of_requests"]) for r in peak_rows if r["cpu_pct_of_requests"]]
    peak_mem = [float(r["mem_mib"]) for r in peak_rows if r["mem_mib"]]

    # scaling reaction: first sample where spec > baseline(2) after ramp start
    react = None
    for r in samples:
        rel = int(r["time"]) - t0
        if rel >= peak_start and int(r["spec_replicas"]) > 2:
            react = rel - peak_start
            break

    # provisioned CPU-seconds: replicas × request(500m=0.5 core) × 10 s
    prov = sum(int(r["spec_replicas"]) for r in samples) * 0.5 * 10

    # pod startup times (creation → PodReady) from pods-final.json
    startups = []
    pods = d.get("pods-final", {})
    for p in pods.get("items", []):
        created = p["metadata"]["creationTimestamp"]
        for c in p.get("status", {}).get("conditions", []):
            if c["type"] == "Ready" and c["status"] == "True":
                from datetime import datetime
                fmt = "%Y-%m-%dT%H:%M:%SZ"
                delta = (datetime.strptime(c["lastTransitionTime"], fmt)
                         - datetime.strptime(created, fmt)).total_seconds()
                startups.append(delta)

    out = {
        "label": label,
        "peak_p95_ms": round(s["peak_p95"] * 1000, 1),
        "peak_p99_ms": round(s["peak_p99"] * 1000, 1),
        "peak_avg_ms": round(s["peak_avg"] * 1000, 1),
        "peak_served_rps": round(sum(x["rps"] for x in d["persec"] if 120 <= x["t"] < 300) / 180, 1),
        "error_rate_pct": round(s["errors"] / max(1, s["total_requests"]) * 100, 3),
        "peak_cpu_milli_avg": round(sum(peak_cpu) / max(1, len(peak_cpu)), 0),
        "peak_cpu_pct_of_requests": round(sum(peak_pct) / max(1, len(peak_pct)), 1) if peak_pct else None,
        "peak_mem_mib_avg": round(sum(peak_mem) / max(1, len(peak_mem)), 1) if peak_mem else None,
        "replicas_min": min(specs), "replicas_max": max(specs),
        "replicas_at_peak_avg": round(sum(peak_specs) / max(1, len(peak_specs)), 1),
        "scaling_reaction_s": react,
        "pod_startup_s": round(sum(startups) / len(startups), 1) if startups else None,
        "provisioned_cpu_core_seconds": round(prov, 0),
        "total_requests": s["total_requests"],
    }
    print(json.dumps(out, indent=2))

if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
