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


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--runtime", required=True)
    parser.add_argument("--tls", required=True)
    parser.add_argument("--host", required=True)
    parser.add_argument("--trials", type=int, required=True)
    parser.add_argument("--warmup-seconds", type=int, required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    reasons = []
    runtime = load(args.runtime)
    tls = load(args.tls)
    host = load(args.host)
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
    if host is None:
        reasons.append("host report is missing or invalid")
    elif not host.get("isolated_resources", False):
        reasons.append("host does not prove isolated resources")
    if host is not None and not host.get("dedicated_link", False):
        reasons.append("host does not prove an independent link")
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
