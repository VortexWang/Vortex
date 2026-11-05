#!/usr/bin/env python3
import hashlib
import json
import os
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
tls_mixed = load(out / "tls-mixed-throughput.json")
workload_fields = ("duration_seconds_per_phase", "tcp_streams", "udp_target_mbps")
for field in workload_fields:
    if actual[field] != tls_mixed[field]:
        raise SystemExit(f"runtime/TLS mixed workload mismatch for {field}: {actual[field]} != {tls_mixed[field]}")
if actual["mixed"]["errors"] or tls_mixed["mixed"]["errors"]:
    raise SystemExit("runtime/TLS mixed workload reported transport errors")
if actual["mixed"]["tcp"]["errors"] or tls_mixed["mixed"]["tcp"]["errors"]:
    raise SystemExit("runtime/TLS mixed TCP workload reported errors")
same_workload_ratio = actual["mixed"]["tcp"]["throughput_mbps"] / tls_mixed["mixed"]["tcp"]["throughput_mbps"]
cross_workload_ratio = actual["mixed"]["tcp"]["throughput_mbps"] / baseline["throughput_mbps"]
tcp_only_ratio = actual["baseline"]["throughput_mbps"] / baseline["throughput_mbps"]
latency = load(out / "runtime-latency.json")
tls_latency = load(out / "tls-latency.json")
faults = load(out / "forged-open.json")
integrity = load(out / "binary-integrity.json")
if integrity["status"] != "pass": raise SystemExit("binary integrity failed")
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
    "independent_tls_mixed": {"tcp_mbps": tls_mixed["mixed"]["tcp"]["throughput_mbps"],
        "udp_loss": tls_mixed["mixed"]["udp_loss"], "udp_target_mbps": tls_mixed["udp_target_mbps"],
        "duration_seconds_per_phase": tls_mixed["duration_seconds_per_phase"], "tcp_streams": tls_mixed["tcp_streams"]},
    "same_workload_ratio": {"runtime_mixed_tcp_to_tls_mixed_tcp": same_workload_ratio,
        "threshold": 0.70, "threshold_met_in_this_diagnostic": same_workload_ratio >= 0.70,
        "basis": "same duration, TCP stream count, UDP offered rate and path"},
    "legacy_ratio_scope":"runtime TCP under mixed load divided by TLS TCP-only; retained as a stricter cross-workload diagnostic",
    "runtime_mixed_to_tls_ratio": cross_workload_ratio,
    "runtime_tcp_only_to_tls_ratio": tcp_only_ratio,
    "latency": {"runtime_median_ms": latency["median_ms"], "tls_median_ms": tls_latency["median_ms"],
        "segment_median_sum_ms": rtt_sum, "runtime_minus_segment_sum_ms": latency["median_ms"] - rtt_sum,
        "segments": segments, "median_difference_ms": latency["median_ms"] - tls_latency["median_ms"],
        "measurement": "client connect to first byte; raw samples retained",
        "nfr_007": "not-qualified: five userspace segment RTT samples and fresh-runtime first-byte samples recorded; pool and mux not separately covered"},
    "binary_integrity": integrity, "direct_bypass": bypass, "forged_open": faults, "middle_hop_failure": middle,
    "authorization_mode": rhost.get("authorization_mode"),
    "performance_qualification": "pass" if (
        os.getenv("PHASE4_FORMAL") == "1" and
        rhost.get("authorization_mode") == "runtime-authority" and
        actual.get("duration_seconds_per_phase", 0) >= 300 and
        actual.get("tcp_streams", 0) >= 16 and
        actual.get("udp_target_mbps", 0) >= 100 and
        actual["mixed"]["tcp"].get("throughput_mbps", 0) >= 400 and
        actual["mixed"].get("udp_sent", 0) * 1200 * 8 /
            max(actual.get("duration_seconds_per_phase", 0), 0.001) / 1_000_000 >= 99 and
        actual["mixed"].get("udp_loss", 1) <= 0.001 and
        tls_mixed["mixed"]["tcp"].get("throughput_mbps", 0) >= 400 and
        tls_mixed["mixed"].get("udp_sent", 0) * 1200 * 8 /
            max(tls_mixed.get("duration_seconds_per_phase", 0), 0.001) / 1_000_000 >= 99 and
        same_workload_ratio >= 0.70 and
        tls_mixed.get("udp_loss", 1) <= 0.001 and
        not rhost.get("dirty", True) and
        not chost.get("dirty", True) and not thost.get("dirty", True) and
        not actual["mixed"].get("errors") and not tls_mixed["mixed"].get("errors")
    ) else "not-qualified",
    "qualification_metrics": {
        "runtime_mixed_tcp_mbps": actual["mixed"]["tcp"].get("throughput_mbps", 0),
        "tls_mixed_tcp_mbps": tls_mixed["mixed"]["tcp"].get("throughput_mbps", 0),
        "runtime_udp_offered_mbps": actual["mixed"].get("udp_sent", 0) * 1200 * 8 /
            max(actual.get("duration_seconds_per_phase", 0), 0.001) / 1_000_000,
        "tls_udp_offered_mbps": tls_mixed["mixed"].get("udp_sent", 0) * 1200 * 8 /
            max(tls_mixed.get("duration_seconds_per_phase", 0), 0.001) / 1_000_000,
        "runtime_udp_loss": actual["mixed"].get("udp_loss", 1),
        "tls_udp_loss": tls_mixed["mixed"].get("udp_loss", 1),
        "same_workload_ratio": same_workload_ratio,
    },
    "qualification": "formal runtime-authority evidence" if os.getenv("PHASE4_FORMAL") == "1" else "not-qualified: short shared-host diagnostic; host/duration, segment RTT, mode coverage and D-04 review remain",
}
(out / "comparison.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
with (out / "checksums.sha256").open("w") as f:
    for path in sorted(out.rglob("*.json")) + sorted((out / "logs").glob("*.log")) + sorted((out / "runtime-logs").glob("*.log")):
        if "config" not in path.parts:
            f.write(hashlib.sha256(path.read_bytes()).hexdigest() + "  " + str(path.relative_to(out)) + "\n")
print(json.dumps(report, sort_keys=True))
