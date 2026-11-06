#!/usr/bin/env python3
"""Audit sampled D-04 conservation and post-workload idle shrink evidence."""
import argparse
import json
from pathlib import Path

AMOUNTS = ("unissued_bytes", "pending_grant_bytes", "unknown_grant_bytes",
           "available_bytes", "held_bytes", "used_bytes", "unknown_bytes")


def audit(timeline, ceiling, require_idle=False, target_seconds=None):
    if target_seconds is not None and (type(target_seconds) is not int or not 1 <= target_seconds <= 60):
        raise ValueError("invalid target horizon")
    if timeline.get("dropped_periodic_samples") != 0:
        raise ValueError("missing periodic samples")
    samples = timeline.get("samples")
    if not isinstance(samples, list) or len(samples) < 2:
        raise ValueError("insufficient timeline")
    previous_time = -1
    previous_used = 0
    budget = None
    identity = None
    targets = []
    formula_samples = uncapped_formula_samples = 0
    if target_seconds is not None and samples[0]["state"]["used_bytes"] != 0:
        raise ValueError("cold-start consumption evidence missing")
    for sample in samples:
        elapsed = sample["elapsed_nanoseconds"]
        state = sample["state"]
        if type(elapsed) is not int or elapsed < 0 or elapsed < previous_time:
            raise ValueError("invalid timeline order")
        previous_time = elapsed
        values = [state[k] for k in (*AMOUNTS, "budget_bytes", "sessions", "dynamic_target_bytes")]
        if any(type(v) is not int or v < 0 for v in values):
            raise ValueError("invalid timeline amount")
        if budget is None:
            budget, identity = state["budget_bytes"], state["snapshot_id"]
        if state["budget_bytes"] != budget or state["snapshot_id"] != identity:
            raise ValueError("timeline authority identity changed")
        if sum(state[k] for k in AMOUNTS) != budget:
            raise ValueError("sample violates source conservation")
        if state["used_bytes"] < previous_used:
            raise ValueError("committed usage decreased")
        previous_used = state["used_bytes"]
        target = state["dynamic_target_bytes"]
        if not 8 * 1024 * 1024 <= target <= ceiling:
            raise ValueError("dynamic target outside floor/ceiling")
        if target_seconds is not None and elapsed < 58_000_000_000:
            # The cold-start timeline begins before usage. Before 58s no
            # one-second bucket can have aged out of the rolling minute.
            expected = min(ceiling, max(8 * 1024 * 1024,
                           (state["used_bytes"] * target_seconds + 59) // 60))
            if target != expected:
                raise ValueError("dynamic target differs from committed consumption horizon")
            formula_samples += 1
            uncapped_formula_samples += 8 * 1024 * 1024 < target < ceiling
        targets.append(target)
    final = samples[-1]["state"]
    if require_idle:
        if samples[-1]["elapsed_nanoseconds"] - samples[0]["elapsed_nanoseconds"] < 65_000_000_000:
            raise ValueError("idle timeline too short")
        if max(targets) <= 8 * 1024 * 1024 or targets[-1] != 8 * 1024 * 1024:
            raise ValueError("target growth and shrink not observed")
        if any(final[k] != 0 for k in ("held_bytes", "pending_grant_bytes", "unknown_grant_bytes", "unknown_bytes", "sessions")):
            raise ValueError("idle authority not settled")
    return {"status": "pass", "samples": len(samples), "target_min": min(targets),
            "target_max": max(targets), "target_final": targets[-1],
            "sampled_source_conserved": True, "idle_shrink_checked": require_idle,
            "issuer_credit_return_proved": False, "formula_samples": formula_samples,
            "uncapped_formula_samples": uncapped_formula_samples}


def main():
    parser = argparse.ArgumentParser(__doc__)
    parser.add_argument("timeline", type=Path)
    parser.add_argument("--ceiling", type=int, required=True)
    parser.add_argument("--require-idle", action="store_true")
    parser.add_argument("--target-seconds", type=int)
    args = parser.parse_args()
    if args.require_idle:
        window = json.loads((args.timeline.parent / "idle-window.json").read_text())
        if window.get("requested_seconds", 0) < 65 or window.get("elapsed_nanoseconds", 0) < 65_000_000_000:
            raise ValueError("65-second post-workload idle window is missing")
    print(json.dumps(audit(json.loads(args.timeline.read_text()), args.ceiling, args.require_idle, args.target_seconds), sort_keys=True))


if __name__ == "__main__":
    main()
