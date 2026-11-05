#!/usr/bin/env python3
"""Audit typed unknown-liability handoff snapshots from Docker and Actions."""

import gzip
import hashlib
import json
import pathlib
import sqlite3
import sys
import tempfile


def audit(root, commit):
    snapshots = []
    for environment in ("docker", "actions"):
        folder = root / environment
        report = json.loads((folder / "report.json").read_text())
        assert report["commit"] == commit and report["status"] == "passed"
        assert report["full_disk_requested"] and not report["plan_complete"]
        metadata = json.loads((folder / "unknown-evidence.json").read_text())
        assert metadata["fault"] == "lost-ack" and metadata["acknowledged"]
        assert metadata["origin_boot_id"] != metadata["sender_boot_id"]
        assert metadata["liability"] == "unresolved" and metadata["unknown_upper_bound"] == 128
        manifest = json.loads((folder / "unknown-ledgers.json").read_text())
        assert set(manifest) == {"agent", "hub"}
        identity = None
        for role, checksum in manifest.items():
            raw = gzip.decompress((folder / f"unknown-{role}.sqlite.gz").read_bytes())
            assert hashlib.sha256(raw).hexdigest() == checksum
            with tempfile.TemporaryDirectory() as tmp:
                path = pathlib.Path(tmp) / "ledger.db"
                path.write_bytes(raw)
                with sqlite3.connect(f"file:{path}?mode=ro&immutable=1", uri=True) as db:
                    assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
                    rows = db.execute(
                        "SELECT node_id,batch_id,origin_boot_id,snapshot_id,grant_id,"
                        "rule_id,direction,unknown_upper_bound,payload,payload_sha256,"
                        "transport_state,liability_state FROM unknown_usage_batches"
                    ).fetchall()
                    assert len(rows) == 1
                    row = rows[0]
                    assert row[2] == metadata["origin_boot_id"] and int(row[7]) == 128
                    assert hashlib.sha256(row[8]).digest() == row[9]
                    assert row[11] == "unresolved"
                    expected = "acknowledged" if role == "agent" else "received"
                    assert row[10] == expected
                    assert db.execute("SELECT COUNT(*) FROM usage_batches").fetchone()[0] == 0
                    if identity is None:
                        identity = row[:10]
                    else:
                        assert identity == row[:10]
                    if role == "agent":
                        assert db.execute("SELECT initial_bytes,remaining_bytes FROM authorization_balances").fetchall() == [("4096", "3968")]
                        assert db.execute("SELECT state,reserved_bytes FROM authorization_usage_holds").fetchall() == [("unknown", "128")]
                        assert db.execute("SELECT state FROM authorization_records WHERE authorization_id=?", (row[4],)).fetchone()[0] == "settlement_only"
                    snapshots.append({"environment": environment, "role": role, "sha256": checksum,
                                      "unknown_upper_bound": 128, "transport_state": expected,
                                      "liability_state": "unresolved", "known_consumption": 0})
    result = {"status": "passed", "commit": commit, "snapshots": snapshots,
              "scope": "typed-unknown-lost-ack-restart", "plan_complete": False}
    (root / "unknown-audit.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"status": "passed", "commit": commit, "snapshots": len(snapshots)}))


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("usage: audit_unknown_usage.py EVIDENCE_ROOT FULL_SHA")
    audit(pathlib.Path(sys.argv[1]), sys.argv[2])
