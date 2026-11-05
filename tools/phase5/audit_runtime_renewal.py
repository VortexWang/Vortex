#!/usr/bin/env python3
"""Independently audit the closed SQLite files from the renewal fixture.

Usage: python3 tools/phase5/audit_runtime_renewal.py EVIDENCE_ROOT FULL_SHA
EVIDENCE_ROOT contains docker/ and actions/ downloaded artifact directories.
This proves only the finite static-destination renewal fixture, not plan completion.
"""

import datetime
import gzip
import hashlib
import json
import pathlib
import re
import sqlite3
import sys
import tempfile


def instant(value):
    # Go writes nanoseconds; Python 3.9 accepts only microsecond precision.
    # Fixture boundaries differ by seconds, so truncation cannot invert them.
    value = re.sub(r"(\.\d{6})\d+", r"\1", value)
    return datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))


def audit_agent(db):
    assert db.execute("SELECT DISTINCT issuer_id FROM authorization_records").fetchall() == [("hub",)]
    balances = db.execute("SELECT initial_bytes,remaining_bytes FROM authorization_balances").fetchall()
    assert len(balances) == 2 and all((int(i), int(r)) == (4096, 4050) for i, r in balances)
    sessions = db.execute(
        "SELECT grant_id,runtime_lease_id,share_lease_id,initial_grant_id,"
        "initial_runtime_lease_id,initial_share_lease_id,state FROM runtime_sessions"
    ).fetchall()
    assert len(sessions) == 1
    session = sessions[0]
    # A close attempted during SQLITE_FULL can conservatively retain the
    # slot. Only later recovery with execution proof may release that slot.
    assert session[6] in {"closed", "open"}
    assert all(session[i] != session[i + 3] for i in range(3))
    current, initial = session[0], session[3]
    expiry = instant(db.execute(
        "SELECT expires_at FROM authorization_records WHERE authorization_id=?", (initial,)
    ).fetchone()[0])
    holds = db.execute(
        "SELECT h.authorization_id,u.used_bytes,u.updated_at,h.state,u.state "
        "FROM authorization_usage_holds h JOIN usage_reservations u "
        "ON h.node_id=u.node_id AND h.batch_id=u.batch_id"
    ).fetchall()
    assert len(holds) == 4
    counts = {initial: 0, current: 0}
    for grant, used, at, hold_state, usage_state in holds:
        assert int(used) == 23 and hold_state == usage_state == "committed"
        counts[grant] += 1
        if grant == initial:
            assert instant(at) < expiry
        else:
            assert instant(at) >= expiry
    assert counts == {initial: 2, current: 2}
    states = db.execute("SELECT DISTINCT state FROM usage_batches").fetchall()
    assert states == [("hub_acked",)]
    assert db.execute("SELECT COUNT(*) FROM runtime_rate_buckets").fetchone()[0] == 1
    return {"issued": 8192, "remaining": 8100, "used": 92,
            "same_session_renewal": True, "post_expiry_batches": 2, "issuer": "hub",
            "session_slot_state": session[6], "retained_slot_requires_recovery": session[6] == "open"}


def audit_hub(db):
    remaining = int(db.execute("SELECT remaining_bytes FROM issuer_delegations").fetchone()[0])
    issues = db.execute("SELECT amount_bytes,payload,payload_sha256 FROM issuer_issues").fetchall()
    assert len(issues) == 2 and remaining == 4096
    assert all(int(n) == 4096 and hashlib.sha256(p).digest() == d for n, p, d in issues)
    assert db.execute("SELECT DISTINCT state FROM usage_batches").fetchall() == [("ledgered",)]
    return {"initial": 12288, "issued": 8192, "remaining": remaining}


def audit(root, expected):
    results = []
    assert len(expected) == 40 and all(c in "0123456789abcdef" for c in expected)
    for environment in ("docker", "actions"):
        folder = root / environment
        report = json.loads((folder / "report.json").read_text())
        assert report["commit"] == expected and report["status"] == "passed"
        assert report["full_disk_requested"] and not report["plan_complete"]
        manifests = sorted(folder.glob("runtime-[12]-ledgers.json"))
        assert [p.name for p in manifests] == ["runtime-1-ledgers.json", "runtime-2-ledgers.json"]
        for manifest in manifests:
            hashes = json.loads(manifest.read_text())
            assert set(hashes) == {"agent", "hub"}
            for role, digest in hashes.items():
                path = manifest.with_name(manifest.name.replace("ledgers.json", role + ".sqlite.gz"))
                raw = gzip.decompress(path.read_bytes())
                assert hashlib.sha256(raw).hexdigest() == digest
                entry = {"environment": environment, "fixture": manifest.stem,
                         "role": role, "sha256": digest}
                with tempfile.TemporaryDirectory() as tmp:
                    ledger = pathlib.Path(tmp) / "ledger.db"
                    ledger.write_bytes(raw)
                    with sqlite3.connect(f"file:{ledger}?mode=ro&immutable=1", uri=True) as db:
                        assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
                        usage = db.execute("SELECT payload,payload_sha256,unknown_upper_bound FROM usage_batches").fetchall()
                        assert len(usage) == 4
                        assert all(hashlib.sha256(p).digest() == d and int(u) == 0 for p, d, u in usage)
                        entry["known_usage_batches"] = 4
                        entry.update(audit_agent(db) if role == "agent" else audit_hub(db))
                results.append(entry)
    assert len(results) == 8
    output = {"status": "passed", "commit": expected, "scope": "finite-runtime-renewal-fixture",
              "plan_complete": False, "snapshots": results}
    (root / "ledger-audit.json").write_text(json.dumps(output, indent=2) + "\n")
    print(json.dumps({"status": "passed", "commit": expected, "snapshots": len(results)}))


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    audit(pathlib.Path(sys.argv[1]), sys.argv[2])
