#!/usr/bin/env python3
"""Run the durable D-04 candidate matrix without overwriting evidence."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
BLOCKS = (64 * 1024, 256 * 1024, 1024 * 1024)
TARGET_SECONDS = (10, 30, 60)
RENEW_PERCENT = (20, 30, 50)


def run_case(case: dict, output: Path, go: str) -> dict:
    if output.exists():
        raise SystemExit(f"refusing to overwrite evidence: {output}")
    output.mkdir(parents=True)
    result_dir = output / "result"
    env = dict(os.environ)
    env.update(
        VORTEX_D04_DURABLE="1",
        VORTEX_D04_BLOCK_BYTES=str(case["block_bytes"]),
        VORTEX_D04_TARGET_SECONDS=str(case["target_seconds"]),
        VORTEX_D04_RENEW_PERCENT=str(case["renew_percent"]),
        VORTEX_D04_RESULT_DIR=str(result_dir),
    )
    command = [go, "test", "./agent", "-run", "^TestRuntimeDurableD04Candidate$", "-count=1", "-v"]
    started = time.time()
    completed = subprocess.run(command, cwd=ROOT, env=env, text=True, capture_output=True, check=False)
    (output / "test.stdout").write_text(completed.stdout)
    (output / "test.stderr").write_text(completed.stderr)
    passed = completed.returncode == 0
    networks = {}
    for path in sorted(result_dir.glob("network-*.json")):
        networks[path.stem.removeprefix("network-")] = json.loads(path.read_text())
    required = {"1", "2"}
    complete = required == set(networks)
    semantic_pass = complete and all(
        item.get("source_conserved")
        and item.get("source_settled")
        and item.get("pending_unknown_held_zero")
        and item.get("issuer_conserved")
        and item.get("issuer_occupancy_bytes") == 0
        and item.get("idle_tail_reclaimed")
        and item.get("pending_demand_settled")
        and item.get("pending_usage_settled")
        for item in networks.values()
    )
    summary = {
        "schema": 1,
        "case": case,
        "status": "pass" if passed and semantic_pass else "fail",
        "exit_code": completed.returncode,
        "duration_seconds": round(time.time() - started, 3),
        "networks": networks,
        "checks": {
            "go_test_passed": passed,
            "both_networks_present": complete,
            "durable_conservation_and_settlement": semantic_pass,
        },
    }
    (output / "summary.json").write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"id": case["id"], "status": summary["status"], "duration_seconds": summary["duration_seconds"]}), flush=True)
    return summary


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--go", default=os.environ.get("GO", "go"))
    args = parser.parse_args()
    base = args.output.resolve()
    if base.exists() and any(base.iterdir()):
        raise SystemExit(f"refusing to overwrite non-empty evidence root: {base}")
    base.mkdir(parents=True, exist_ok=True)
    cases = []
    number = 0
    for block in BLOCKS:
        for target in TARGET_SECONDS:
            for renew in RENEW_PERCENT:
                number += 1
                cases.append({"id": f"d04-durable-{number:02d}", "block_bytes": block, "target_seconds": target, "renew_percent": renew})
    manifest = {"schema": 1, "status": "running", "locked": False, "commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(), "cases": cases}
    (base / "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    summaries = []
    for case in cases:
        summaries.append(run_case(case, base / case["id"], args.go))
    status = "pass" if all(item["status"] == "pass" for item in summaries) else "fail"
    final = {**manifest, "status": status, "locked": False, "summaries": summaries}
    (base / "summary.json").write_text(json.dumps(final, indent=2, sort_keys=True) + "\n")
    digest = hashlib.sha256((base / "summary.json").read_bytes()).hexdigest()
    (base / "summary.sha256").write_text(f"{digest}  summary.json\n")
    print(json.dumps({"status": status, "cases": len(summaries), "locked": False, "summary_sha256": digest}))
    return 0 if status == "pass" else 1


if __name__ == "__main__":
    sys.exit(main())
