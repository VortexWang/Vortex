#!/usr/bin/env python3
"""Fail-closed gate for formal Phase 4 performance qualification."""
import argparse
import json
from pathlib import Path


def load(path: str):
    try:
        return json.loads(Path(path).read_text())
    except (OSError, ValueError):
        return None


def memory_gib(host):
    value = host.get("memory_gib")
    if isinstance(value, (int, float)):
        return float(value)
    text = str(host.get("mem_total", ""))
    parts = text.split()
    if len(parts) >= 2 and parts[0] == "MemTotal:":
        try:
            return float(parts[1]) / 1024 / 1024
        except ValueError:
            pass
    return None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--runtime", required=True)
    parser.add_argument("--tls", required=True)
    parser.add_argument("--host", required=True)
    parser.add_argument("--trials", type=int, required=True)
    parser.add_argument("--trial-reports", nargs="+", required=True,
                        help="runtime reports for individual formal trials")
    parser.add_argument("--warmup-report", required=True,
                        help="runtime report from the 60-second formal warmup")
    parser.add_argument("--warmup-seconds", type=int, required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    reasons = []
    runtime = load(args.runtime)
    tls = load(args.tls)
    host = load(args.host)
    warmup = load(args.warmup_report)
    if runtime is None:
        reasons.append("runtime report is missing or invalid")
    elif runtime.get("performance_qualification") != "pass":
        reasons.append("runtime performance report is not pass")
    if runtime is not None and runtime.get("authorization_mode") != "runtime-authority":
        reasons.append("runtime authority gate is missing")
    if tls is None:
        reasons.append("independent TLS report is missing or invalid")
    elif tls.get("performance_qualification") != "pass":
        reasons.append("independent TLS report is not pass")
    if runtime is not None and tls is not None:
        if not runtime.get("commit") or runtime.get("commit") != tls.get("commit"):
            reasons.append("runtime and independent TLS commit identity differs")
        if (not runtime.get("semantic_manifest_sha256") or
                runtime.get("semantic_manifest_sha256") != tls.get("semantic_manifest_sha256")):
            reasons.append("runtime and independent TLS workload identity differs")
        if runtime.get("dirty") is not False or tls.get("dirty") is not False:
            reasons.append("qualification evidence was collected from a dirty worktree")
    if args.trials > 0:
        trial_reports = [load(path) for path in args.trial_reports]
        trial_reports = [report for report in trial_reports if report is not None]
        if len(trial_reports) != args.trials:
            reasons.append("runtime trial report count differs from the requested trial count")
        trial_ids = [report.get("trial_id") for report in trial_reports]
        if any(not trial_id for trial_id in trial_ids) or len(set(trial_ids)) != len(trial_ids):
            reasons.append("runtime trial report IDs are missing or duplicated")
        for report in trial_reports:
            if report.get("performance_qualification") != "pass":
                reasons.append("one or more runtime trial reports are not pass")
                break
            if report.get("authorization_mode") != "runtime-authority":
                reasons.append("one or more runtime trial reports lack runtime authority")
                break
            metrics = report.get("qualification_metrics", {})
            if (metrics.get("duration_seconds", 0) < 300 or metrics.get("tcp_streams", 0) < 16 or
                    metrics.get("mixed_tcp_mbps", 0) < 400 or metrics.get("udp_offered_mbps", 0) < 99 or
                    metrics.get("udp_loss", 1) > 0.001):
                reasons.append("one or more runtime trial metrics miss formal thresholds")
                break
            if (runtime is not None and
                    (report.get("commit") != runtime.get("commit") or
                     report.get("semantic_manifest_sha256") != runtime.get("semantic_manifest_sha256") or
                     report.get("dirty") is not False)):
                reasons.append("runtime trial reports do not share clean commit and workload identity")
                break
        if runtime is not None and runtime.get("trial_id") not in trial_ids:
            reasons.append("runtime report is not one of the submitted trial reports")
    if warmup is None:
        reasons.append("formal warmup report is missing or invalid")
    else:
        if warmup.get("runtime_correctness") != "pass" or warmup.get("authorization_mode") != "runtime-authority":
            reasons.append("formal warmup report did not pass under runtime authority")
        if warmup.get("commit") != (runtime or {}).get("commit") or warmup.get("dirty") is not False:
            reasons.append("formal warmup does not share the clean runtime commit")
        if warmup.get("qualification_metrics", {}).get("duration_seconds", 0) < 60:
            reasons.append("formal warmup report is shorter than 60 seconds")
    if tls is not None:
        metrics = tls.get("qualification_metrics", {})
        if (metrics.get("runtime_mixed_tcp_mbps", 0) < 400 or metrics.get("tls_mixed_tcp_mbps", 0) < 400 or
                metrics.get("runtime_udp_offered_mbps", 0) < 99 or metrics.get("tls_udp_offered_mbps", 0) < 99 or
                metrics.get("runtime_udp_loss", 1) > 0.001 or metrics.get("tls_udp_loss", 1) > 0.001 or
                metrics.get("same_workload_ratio", 0) < 0.70):
            reasons.append("independent TLS comparison metrics miss formal thresholds")
    if host is None:
        reasons.append("host report is missing or invalid")
    else:
        if int(host.get("cpu_count", 0)) < 4:
            reasons.append("host has fewer than 4 logical CPUs")
        memory = memory_gib(host)
        if memory is None:
            reasons.append("host memory evidence is missing")
        elif memory < 8:
            reasons.append("host has less than 8 GiB memory")
        if host.get("system") != "Linux" or host.get("os_id") != "ubuntu" or host.get("os_version") != "24.04":
            reasons.append("formal host must be Ubuntu 24.04 Linux")
        if host.get("machine") not in ("x86_64", "aarch64", "amd64", "arm64"):
            reasons.append("formal host architecture is unsupported")
        if host.get("backend") != "native" or host.get("driver_placement") != "native-host":
            reasons.append("formal evidence was not collected on the native Linux host")
        if not host.get("qualification_interface") or host.get("qualification_interface") == "lo":
            reasons.append("formal host non-loopback qualification interface is missing")
    if args.warmup_seconds < 60:
        reasons.append("warmup is shorter than 60 seconds")
    if args.trials < 3:
        reasons.append("fewer than three qualification trials")
    result = {
        "schema": 1,
        "status": "pass" if not reasons else "not-qualified",
        "trials": args.trials,
        "warmup_seconds": args.warmup_seconds,
        "reasons": reasons,
    }
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps(result, sort_keys=True))
    return 0 if not reasons else 1


if __name__ == "__main__":
    raise SystemExit(main())
