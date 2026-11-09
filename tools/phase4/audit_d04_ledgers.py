#!/usr/bin/env python3
"""Independently reconcile closed SQLite copies from finite durable D-04 cases."""
import argparse
import contextlib
import gzip
import hashlib
import json
from pathlib import Path
import sqlite3
import tempfile


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def audit_case(folder):
    summary = json.loads((folder / "summary.json").read_text())
    require(summary["status"] == "pass", "case did not pass")
    results = []
    for network in (1, 2):
        label = f"runtime-block-{network}-rounds-0-delay-false"
        ledger_root = folder / "ledgers"
        hashes = json.loads((ledger_root / f"{label}-ledgers.json").read_text())
        require(set(hashes) == {"agent", "hub", "backend"}, "missing ledger role")
        with tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
            dbs = {}
            batches = {}
            for role in ("agent", "hub", "backend"):
                raw = gzip.decompress((ledger_root / f"{label}-{role}.sqlite.gz").read_bytes())
                require(hashlib.sha256(raw).hexdigest() == hashes[role], "ledger hash mismatch")
                path = Path(tmp) / f"{role}.sqlite"
                path.write_bytes(raw)
                db = stack.enter_context(contextlib.closing(sqlite3.connect(f"file:{path}?mode=ro&immutable=1", uri=True)))
                dbs[role] = db
                require(db.execute("PRAGMA integrity_check").fetchall() == [("ok",)], "SQLite integrity failure")
                rows = db.execute("SELECT node_id,batch_id,payload,payload_sha256,state,unknown_upper_bound FROM usage_batches").fetchall()
                require(bool(rows), "empty usage ledger")
                expected = "hub_acked" if role == "agent" else "ledgered"
                entries = {}
                for node, batch, payload, digest, state, unknown in rows:
                    require(state == expected and int(unknown) == 0, "unsettled usage batch")
                    require(hashlib.sha256(payload).digest() == digest, "usage payload hash mismatch")
                    entries[(node, batch)] = payload
                batches[role] = entries
            require(batches["agent"] == batches["hub"] == batches["backend"], "three-ledger batch identity/payload mismatch")
            agent, hub = dbs["agent"], dbs["hub"]
            require(agent.execute("SELECT COUNT(*) FROM authorization_demands WHERE state='pending'").fetchone()[0] == 0, "pending demand remains")
            sources = agent.execute("SELECT node_id,source_id,initial_bytes FROM authorization_source_owners").fetchall()
            require(len(sources) == 1, "unexpected source count")
            node, source, initial = sources[0]
            initial = int(initial)
            balances = agent.execute("SELECT initial_bytes,remaining_bytes FROM authorization_balances WHERE node_id=? AND source_id=?", (node, source)).fetchall()
            require(all(0 <= int(r) <= int(i) for i, r in balances), "invalid grant balance")
            require(sum(int(i) for i, _ in balances) == initial, "source ownership differs from grants")
            remaining = sum(int(r) for _, r in balances)
            holds = agent.execute("SELECT h.state,h.reserved_bytes,u.used_bytes FROM authorization_usage_holds h JOIN usage_reservations u ON h.node_id=u.node_id AND h.batch_id=u.batch_id").fetchall()
            require(all(state in {"committed", "returned"} for state, _, _ in holds), "held or unknown usage remains")
            require(all(0 <= int(used) <= int(reserved) for _, reserved, used in holds), "invalid hold amount")
            committed = sum(int(used) for state, _, used in holds if state == "committed")
            require(initial == remaining + committed, "source conservation failed")
            issues = hub.execute("SELECT amount_bytes,payload,payload_sha256 FROM issuer_issues").fetchall()
            require(all(hashlib.sha256(p).digest() == h for _, p, h in issues), "issuer payload hash mismatch")
            issued = sum(int(amount) for amount, _, _ in issues)
            delegations = hub.execute("SELECT payload,remaining_bytes FROM issuer_delegations").fetchall()
            require(len(delegations) == 1, "unexpected delegation count")
            delegation_raw, unissued = delegations[0]
            delegation = json.loads(delegation_raw)
            require(int(delegation["RawBytes"]) == int(unissued) + issued, "Hub delegation conservation failed")
            require(issued == initial, "Hub/Agent issued amounts differ")
            reported = summary["networks"][str(network)]
            require(reported["committed_bytes"] == committed and reported["issued_bytes"] == issued, "summary differs from SQLite")
            results.append({"network": network, "batches": len(batches["agent"]), "issued_bytes": issued, "committed_bytes": committed, "remaining_bytes": remaining, "ledger_hashes": hashes})
    return {"status": "pass", "scope": "finite closed SQLite reconciliation", "case": summary["case"], "networks": results}


def main():
    parser = argparse.ArgumentParser(__doc__)
    parser.add_argument("case_dir", type=Path)
    args = parser.parse_args()
    print(json.dumps(audit_case(args.case_dir), indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
