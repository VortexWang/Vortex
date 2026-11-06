#!/usr/bin/env python3
"""Execute one-factor D-04 trials; never automatically lock production defaults."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]


def candidates(rate_mbps=100):
    baseline = dict(block_bytes=256 * 1024, grant_target_seconds=30, waterline_percent=30)
    cases = [dict(id="baseline", **baseline)]
    for field, values in (("block_bytes", (64 * 1024, 1024 * 1024)),
                          ("grant_target_seconds", (10, 60)),
                          ("waterline_percent", (20, 50))):
        for value in values:
            cases.append(dict(baseline, id=f"{field}-{value}", **{field: value}))
    for case in cases:
        case["grant_target_bytes"] = max(8 * 1024 * 1024, int(rate_mbps * 1e6 / 8 * case["grant_target_seconds"]))
    return cases


def percentile_bucket(buckets, percentile):
    if (not isinstance(buckets, list) or not buckets or len(buckets) > 64 or
            any(type(value) is not int or value < 0 for value in buckets)):
        raise ValueError("invalid issuer timing histogram")
    if (type(percentile) not in (int, float) or not math.isfinite(percentile) or
            not 0 < percentile <= 1):
        raise ValueError("invalid issuer timing percentile")
    count = sum(buckets)
    if not count:
        raise ValueError("no issuer timing samples")
    threshold = math.ceil(count * percentile)
    total = 0
    for index, count in enumerate(buckets):
        total += count
        if total >= threshold:
            return (2 ** (index + 1) - 1) / 1e6


def evaluate(out):
    credit = json.loads((out / "events-source.jsonl.credit.json").read_text())
    workload = json.loads((out / "mixed-workload.json").read_text())
    host = json.loads((out / "host.json").read_text())
    short = json.loads((out / "short-flow-latency.json").read_text())
    report = json.loads((out / "report.json").read_text())
    obs = credit["observations"]
    total = sum(credit[key] for key in ("unissued_bytes", "pending_grant_bytes", "unknown_grant_bytes", "available_bytes", "held_bytes", "used_bytes", "unknown_bytes"))
    settled = all(credit[key] == 0 for key in ("pending_grant_bytes", "unknown_grant_bytes", "held_bytes", "unknown_bytes", "sessions", "denials"))
    if host["dirty"] or host["authorization_mode"] != "runtime-authority" or total != credit["budget_bytes"] or not settled:
        raise ValueError("unclean, unauthorized or unconserved D-04 trial")
    if report.get("runtime_correctness") != "pass" or not report.get("qualification_metrics", {}).get("source_credit_conserved"):
        raise ValueError("target accounting did not reconcile")
    buckets = obs["issuer_nanoseconds_log2_buckets"]
    if sum(buckets) != obs["issuer_calls"] or obs["issuer_calls"] != credit["grants"]:
        raise ValueError("issuer observation count mismatch")
    return dict(commit=host["commit"], semantic_manifest_sha256=host["semantic_manifest_sha256"],
                issuer_scope="synchronous-process-local-allocator; excludes network/durable Hub wait",
                issuer_wait_upper_bound_ms={str(p): percentile_bucket(buckets, p / 100) for p in (50, 95, 99)},
                reservations_per_block=obs["reservations"] / obs["block_allocations"],
                reservations_per_grant=obs["reservations"] / credit["grants"],
                peak_active_sessions=obs["peak_sessions"], peak_held_bytes=obs["peak_held_bytes"],
                unknown_upper_bound_bytes=credit["unknown_bytes"] + credit["unknown_grant_bytes"],
                source_conserved=True, source_settled=True,
                short_flow_median_ms=short["median_ms"], short_flow_samples_ms=short["samples_ms"],
                tcp_only=workload["baseline"], mixed=workload["mixed"],
                share_drain_scope="not exercised by this pinned-snapshot workload")


def main():
    parser = argparse.ArgumentParser(__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--duration", type=int, default=15)
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--rate-mbps", type=float, default=100)
    args = parser.parse_args()
    if args.duration < 5 or args.repeats < 1 or args.rate_mbps <= 0:
        parser.error("positive rate, duration >= 5, repeats >= 1 required")
    base = args.output.resolve()
    base.mkdir(parents=True, exist_ok=True)
    results = []
    for case in candidates(args.rate_mbps):
        for repeat in range(1, args.repeats + 1):
            out = base / case["id"] / f"trial-{repeat}"
            if out.exists():
                raise SystemExit(f"refusing to overwrite evidence: {out}")
            out.mkdir(parents=True)
            env = dict(os.environ, BACKEND="native", PHASE4_FORMAL="1", PHASE4_TRIAL_ID=f"d04-{case['id']}-{repeat}",
                       QUALIFICATION_DIR=str(out), MIXED_DURATION_SECONDS=str(args.duration), MIXED_TCP_STREAMS="16",
                       MIXED_UDP_MBPS=str(args.rate_mbps), D04_SHORT_SAMPLES="50", PHASE4_CREDIT_OBSERVE="true",
                       PHASE4_CREDIT_INITIAL_BLOCK=str(case["block_bytes"]), PHASE4_CREDIT_MAX_BLOCK=str(4 * 1024 * 1024),
                       PHASE4_CREDIT_GRANT_TARGET=str(case["grant_target_bytes"]), PHASE4_CREDIT_RENEW_PERCENT=str(case["waterline_percent"]))
            print(f"running {case['id']} trial {repeat}", flush=True)
            with (out / "runner.log").open("w") as log:
                subprocess.run([str(ROOT / "tools/phase4/qualification.sh")], env=env, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT, check=True)
            result = dict(case=case, repeat=repeat, **evaluate(out))
            (out / "d04-result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
            results.append(result)
            (base / "progress.json").write_text(json.dumps(results, indent=2, sort_keys=True) + "\n")
    if len({result["commit"] for result in results}) != 1:
        raise SystemExit("mixed source commits")
    summary = dict(schema=1, status="measured-candidates-require-review", locked=False,
                   reference_rate_mbps=args.rate_mbps, duration_seconds=args.duration, repeats=args.repeats, results=results)
    (base / "summary.json").write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
    with (base / "checksums.sha256").open("w") as checksums:
        for path in sorted(base.rglob("*.json")):
            checksums.write(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.relative_to(base)}\n")
    print(json.dumps(dict(status=summary["status"], trials=len(results), locked=False)))


if __name__ == "__main__":
    main()
