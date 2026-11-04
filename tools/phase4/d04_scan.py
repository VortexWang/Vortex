#!/usr/bin/env python3
"""Create the controlled D-04 one-factor candidate matrix.

The matrix is deliberately an evidence plan until each case has raw runtime
authority observations. It never promotes a candidate to a locked default.
"""
import argparse
import hashlib
import json
from pathlib import Path

BLOCKS = [64 * 1024, 256 * 1024, 1024 * 1024]
GRANTS = [10, 30, 60]
WATERLINES = [20, 30, 50]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    cases = []
    case_id = 0
    for block in BLOCKS:
        for grant in GRANTS:
            for waterline in WATERLINES:
                case_id += 1
                cases.append({
                    "id": f"d04-{case_id:02d}",
                    "block_bytes": block,
                    "grant_target_seconds": grant,
                    "waterline_percent": waterline,
                    "minimum_budget_bytes": 8 * 1024 * 1024,
                    "status": "pending-evidence",
                    "locked": False,
                })
    matrix = {
        "schema": 1,
        "status": "candidate-matrix-only",
        "locked": False,
        "factor_policy": {
            "block_bytes": BLOCKS,
            "grant_target_seconds": GRANTS,
            "waterline_percent": WATERLINES,
            "minimum_budget_bytes": 8 * 1024 * 1024,
            "one_factor_at_a_time": True,
        },
        "required_observations": [
            "issuer_wait_p50_p95_p99_ms",
            "merge_rate",
            "active_and_draining_occupancy",
            "unknown_upper_bound_bytes",
            "per_source_conservation",
            "actual_offered_tcp_udp_rate",
        ],
        "cases": cases,
    }
    output.write_text(json.dumps(matrix, indent=2, sort_keys=True) + "\n")
    digest = hashlib.sha256(output.read_bytes()).hexdigest()
    Path(str(output) + ".sha256").write_text(f"{digest}  {output.name}\n")
    print(json.dumps({"cases": len(cases), "locked": False, "sha256": digest}, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
