#!/usr/bin/env python3
"""Qualify separate pool/mux first-byte samples against measured path RTTs."""
import argparse
import hashlib
import math
import json
import statistics
from pathlib import Path


def validate_timeline(samples, timeline):
    if not isinstance(timeline, list) or len(timeline) != len(samples):
        raise ValueError("latency event timeline does not align with samples")
    required = ("sample", "connect_ms", "send_ms", "first_byte_ms", "complete_ms", "close_ms")
    for index, event in enumerate(timeline):
        if not isinstance(event, dict) or event.get("sample") != index:
            raise ValueError("latency event timeline has invalid sample index")
        values = []
        for field in required[1:]:
            value = event.get(field)
            if not isinstance(value, (int, float)) or not math.isfinite(value) or value < 0:
                raise ValueError(f"invalid latency timeline value: {field}")
            values.append(value)
        connect, send, first_byte, complete, close = values
        if not (connect <= first_byte <= complete <= close):
            raise ValueError("latency event timeline is out of order")
        if abs(first_byte - samples[index]) > 1e-6:
            raise ValueError("latency timeline first-byte value differs from sample")


def qualify(base):
    reasons, modes, commits = [], {}, set()
    for mode in ("pool", "mux"):
        try:
            out = base / mode / "comparison"
            report = json.loads((out / "comparison.json").read_text())
            host = json.loads((out / "runtime-host.json").read_text())
            latency = json.loads((out / "runtime-latency.json").read_text())
            samples = latency["samples_ms"]
            validate_timeline(samples, latency.get("timeline"))
            config = json.loads((out / "config/source.json").read_text())
            if host.get("dirty") is not False or host["authorization_mode"] != "runtime-authority" or host["backend"] != "native" or host["system"] != "Linux":
                reasons.append(f"{mode}: requires clean authorized native Linux evidence")
            if config["transport_mode"] != mode or not host.get("commit") or report["commit"] != host["commit"]:
                reasons.append(f"{mode}: mode or commit mismatch")
            if len(samples) < 50:
                reasons.append(f"{mode}: fewer than 50 fresh connections")
            if any(not isinstance(x, (int, float)) or not math.isfinite(x) or x < 0 for x in samples):
                raise ValueError("invalid latency sample")
            median = statistics.median(samples)
            segments = []
            for name in ("source", "relay-1", "relay-2", "relay-3", "destination"):
                segment = json.loads((out / f"rtt-{name}.json").read_text())
                if len(segment["samples_ms"]) < 50:
                    reasons.append(f"{mode}/{name}: fewer than 50 RTT samples")
                if any(not isinstance(x, (int, float)) or not math.isfinite(x) or x < 0 for x in segment["samples_ms"]):
                    raise ValueError("invalid RTT sample")
                measured = statistics.median(segment["samples_ms"])
                if not math.isfinite(segment["median_ms"]) or abs(measured - segment["median_ms"]) > 1e-6 or not 0 <= measured <= 2:
                    reasons.append(f"{mode}/{name}: inconsistent or out-of-profile RTT")
                segments.append(measured)
            extra = median - sum(segments)
            if not math.isfinite(report["latency"]["runtime_minus_segment_sum_ms"]) or abs(extra - report["latency"]["runtime_minus_segment_sum_ms"]) > 1e-6:
                reasons.append(f"{mode}: report differs from raw samples")
            if extra > 5:
                reasons.append(f"{mode}: extra first-byte latency exceeds 5 ms")
            for field in ("binary_integrity", "forged_open", "middle_hop_failure"):
                if report[field]["status"] != "pass":
                    reasons.append(f"{mode}: {field} did not pass")
            if any(report["direct_bypass"][name]["status"] != "pass" for name in ("relay-2", "destination")):
                reasons.append(f"{mode}: direct bypass check failed")
            commits.add(host["commit"])
            modes[mode] = dict(client_first_byte_median_ms=median, segment_median_sum_ms=sum(segments),
                               additional_ms=extra, sample_count=len(samples))
        except (OSError, ValueError, KeyError, TypeError, statistics.StatisticsError) as error:
            reasons.append(f"{mode}: incomplete evidence ({type(error).__name__})")
    if len(commits) != 1:
        reasons.append("pool and mux must share one clean commit")
    return dict(schema=1, status="pass" if not reasons else "not-qualified", reasons=reasons,
                commit=next(iter(commits)) if len(commits) == 1 else None, modes=modes,
                scope="NFR-007 configured five-process route; authorized fresh sessions with reusable links; excludes credit starvation and policy protection waits")


def main():
    parser = argparse.ArgumentParser(__doc__)
    parser.add_argument("--evidence", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    result = qualify(args.evidence)
    result["report_evaluator_sha256"] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps(result, sort_keys=True))
    return 0 if result["status"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
