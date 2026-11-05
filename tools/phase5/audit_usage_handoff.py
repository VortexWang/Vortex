#!/usr/bin/env python3
"""Verify online/autonomous Agent-Hub-backend protocol-fixture ledgers."""
import gzip
import hashlib
import json
import pathlib
import sqlite3
import sys
import tempfile


def audit(root, commit):
    snapshots = []
    for env in ("docker", "actions"):
        folder = root / env
        report = json.loads((folder / "report.json").read_text())
        assert report["commit"] == commit and report["status"] == "passed"
        for kind in ("known", "unknown"):
            for mode in ("online-lost-ack", "autonomous"):
                label = f"handoff-{kind}-{mode}"
                manifest = json.loads((folder / f"{label}-ledgers.json").read_text())
                assert set(manifest) == {"agent", "hub", "backend"}
                original = None
                for role, checksum in sorted(manifest.items()):
                    raw = gzip.decompress((folder / f"{label}-{role}.sqlite.gz").read_bytes())
                    assert hashlib.sha256(raw).hexdigest() == checksum
                    with tempfile.TemporaryDirectory() as tmp:
                        path = pathlib.Path(tmp) / "ledger.db"
                        path.write_bytes(raw)
                        with sqlite3.connect(f"file:{path}?mode=ro&immutable=1", uri=True) as db:
                            assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
                            table = "unknown_usage_batches" if kind == "unknown" else "usage_batches"
                            rows = db.execute(f"SELECT node_id,batch_id,payload,payload_sha256 FROM {table}").fetchall()
                            assert len(rows) == 1
                            row = rows[0]
                            assert hashlib.sha256(row[2]).digest() == row[3]
                            if original is None:
                                original = row
                            else:
                                assert original == row
                            if kind == "unknown":
                                assert db.execute("SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches").fetchone() == ("128", "unresolved")
                                assert db.execute("SELECT COUNT(*) FROM usage_batches").fetchone()[0] == 0
                            if role == "hub":
                                assert db.execute("SELECT node_id,batch_id,kind,payload_sha256 FROM usage_backend_receipts").fetchall() == [(row[0], row[1], kind, row[3])]
                                state = dict(db.execute("SELECT key,value FROM agent_state"))
                                assert state["authority_mode"] == ("autonomous" if mode == "autonomous" else "online")
                                assert state["signer_epoch_open"] == ("false" if mode == "autonomous" else "true")
                            snapshots.append({"environment": env, "kind": kind, "mode": mode, "role": role, "sha256": checksum})
        for network in (1, 2):
            label = f"runtime-handoff-{network}"
            manifest = json.loads((folder / f"{label}-ledgers.json").read_text())
            assert set(manifest) == {"agent", "hub", "backend"}
            original = None
            for role, checksum in sorted(manifest.items()):
                raw = gzip.decompress((folder / f"{label}-{role}.sqlite.gz").read_bytes())
                assert hashlib.sha256(raw).hexdigest() == checksum
                with tempfile.TemporaryDirectory() as tmp:
                    path = pathlib.Path(tmp) / "ledger.db"
                    path.write_bytes(raw)
                    with sqlite3.connect(f"file:{path}?mode=ro&immutable=1", uri=True) as db:
                        assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
                        rows = db.execute("SELECT node_id,batch_id,payload,payload_sha256 FROM usage_batches ORDER BY node_id,batch_id").fetchall()
                        assert len(rows) == 4
                        assert all(hashlib.sha256(r[2]).digest() == r[3] for r in rows)
                        if original is None:
                            original = rows
                        else:
                            assert original == rows
                        if role == "hub":
                            assert db.execute("SELECT node_id,batch_id,kind,payload_sha256 FROM usage_backend_receipts ORDER BY node_id,batch_id").fetchall() == [(r[0], r[1], "known", r[3]) for r in rows]
                        snapshots.append({"environment": env, "kind": "runtime", "network": network, "role": role, "sha256": checksum})
    result = {"status": "passed", "commit": commit, "scope": "protocol-backend-ack-and-autonomous-retention", "snapshots": snapshots}
    (root / "handoff-audit.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"status": "passed", "snapshots": len(snapshots)}))


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("usage: audit_usage_handoff.py EVIDENCE_ROOT FULL_SHA")
    audit(pathlib.Path(sys.argv[1]), sys.argv[2])
