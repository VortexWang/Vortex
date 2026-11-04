#!/usr/bin/env python3
import hashlib
import json
import sys
from pathlib import Path

runtime, out = map(Path, sys.argv[1:])
load = lambda path: json.loads(path.read_text())
rhost = load(runtime / "host.json")
chost = load(out / "runtime-host.json")
thost = load(out / "host.json")
for host in (chost, thost):
    if host.get("runtime_environment") != rhost.get("runtime_environment"):
        raise SystemExit("comparison runtime environment differs from mixed workload")
    if host["commit"] != rhost["commit"] or host["semantic_manifest_sha256"] != rhost["semantic_manifest_sha256"]:
        raise SystemExit("comparison SHA/semantic path differs from runtime workload")
baseline = load(out / "tls-throughput.json")
actual = load(runtime / "mixed-workload.json")
latency = load(out / "runtime-latency.json")
tls_latency = load(out / "tls-latency.json")
faults = load(out / "forged-open.json")
middle = load(out / "middle-hop-fault.json")
bypass = {name: load(out / ("bypass-" + name + ".json")) for name in ("relay-2", "destination")}
if any(v["status"] != "pass" for v in bypass.values()): raise SystemExit("bypass probe failed")
if faults["status"] != "pass" or middle["status"] != "pass":
    raise SystemExit("fault checks not passed")
segments = {p.stem: load(p) for p in out.glob("rtt-*.json")}
if len(segments) != 5: raise SystemExit("missing segment RTT evidence")
rtt_sum = sum(s["median_ms"] for s in segments.values())
report = {
    "schema": 1, "commit": rhost["commit"],
    "runtime_environment": rhost.get("runtime_environment"),
    "dirty": any(h["dirty"] for h in (rhost, chost, thost)),
    "semantic_manifest_sha256": rhost["semantic_manifest_sha256"],
    "independent_tls": {"tcp_mbps": baseline["throughput_mbps"],
        "implementation": "five-process Go TLS 1.3 io.Copy, same endpoint configs, no VLink/mux/accounting"},
    "runtime_mixed_to_tls_ratio": actual["mixed"]["tcp"]["throughput_mbps"] / baseline["throughput_mbps"],
    "runtime_tcp_only_to_tls_ratio": actual["baseline"]["throughput_mbps"] / baseline["throughput_mbps"],
    "latency": {"runtime_median_ms": latency["median_ms"], "tls_median_ms": tls_latency["median_ms"],
        "segment_median_sum_ms": rtt_sum, "runtime_minus_segment_sum_ms": latency["median_ms"] - rtt_sum,
        "segments": segments, "median_difference_ms": latency["median_ms"] - tls_latency["median_ms"],
        "measurement": "client connect to first byte; raw samples retained",
        "nfr_007": "not-qualified: five userspace segment RTT samples and fresh-runtime first-byte samples recorded; pool and mux not separately covered"},
    "direct_bypass": bypass, "forged_open": faults, "middle_hop_failure": middle,
    "qualification": "not-qualified: short shared-host diagnostic; host/duration, segment RTT, mode coverage and D-04 review remain",
}
(out / "comparison.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
with (out / "checksums.sha256").open("w") as f:
    for path in sorted(out.rglob("*.json")) + sorted((out / "logs").glob("*.log")) + sorted((out / "runtime-logs").glob("*.log")):
        if "config" not in path.parts:
            f.write(hashlib.sha256(path.read_bytes()).hexdigest() + "  " + str(path.relative_to(out)) + "\n")
print(json.dumps(report, sort_keys=True))
