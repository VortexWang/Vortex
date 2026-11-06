#!/usr/bin/env python3
"""Re-audit retained Hub cleanup snapshot without importing the Go implementation."""
import gzip
import hashlib
import json
from pathlib import Path
import sqlite3
import sys
import tempfile

out = Path(sys.argv[1])
meta = json.loads((out / "settled-hub.json").read_text())
raw = gzip.decompress((out / "settled-hub.sqlite.gz").read_bytes())
assert meta["sha256"] == hashlib.sha256(raw).hexdigest()
assert meta["production_backend_authority"] is False
assert meta["clock"] == "deterministic-conservative-bounds"
with tempfile.TemporaryDirectory() as directory:
    path = Path(directory) / "hub.db"
    path.write_bytes(raw)
    db = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    assert db.execute("PRAGMA integrity_check").fetchone() == ("ok",)
    assert db.execute("PRAGMA journal_mode").fetchone() == ("wal",)
    assert db.execute("SELECT batch_id FROM usage_batches ORDER BY batch_id").fetchall() == [("no-backend",), ("tail",)]
    tombstones = db.execute("SELECT node_id,batch_id,snapshot_id,payload_sha256,ledger_sequence FROM settled_usage_tombstones").fetchall()
    assert len(tombstones) == 1
    node, batch, snapshot, digest, sequence = tombstones[0]
    assert (node, batch, snapshot) == ("node", "hole", "snapshot")
    assert digest == hashlib.sha256(b"known-hole").digest()
    assert db.execute("SELECT 1 FROM usage_backend_receipts WHERE node_id=? AND batch_id=?", (node, batch)).fetchone() is None
    assert db.execute("SELECT payload,payload_sha256,confirmed FROM hub_ledger WHERE sequence=?", (sequence,)).fetchone() == (b"known-hole", digest, 1)
    checkpoint = int(db.execute("SELECT value FROM agent_state WHERE key='ledger_checkpoint'").fetchone()[0])
    assert checkpoint >= sequence
    assert db.execute("SELECT 1 FROM hub_ledger WHERE sequence<=? AND confirmed=0", (checkpoint,)).fetchone() is None
    assert db.execute("SELECT batch_id,unknown_upper_bound,liability_state FROM unknown_usage_batches").fetchall() == [("unknown", "128", "unresolved")]
    assert db.execute("SELECT batch_id,payload FROM usage_disputes").fetchall() == [("tail", b"dispute evidence")]
    assert db.execute("SELECT COUNT(*) FROM hub_ledger").fetchone()[0] == 4
    for table in ("usage_batches", "hub_ledger", "unknown_usage_batches", "usage_disputes"):
        for payload, expected in db.execute(f"SELECT payload,payload_sha256 FROM {table}"):
            assert hashlib.sha256(payload).digest() == expected, table
    assert db.execute("SELECT COUNT(*) FROM issuer_issues").fetchone()[0] == 0
    db.close()
print(json.dumps({"schema": 1, "status": "passed", "scope": "deterministic-hub-retention", "sha256": meta["sha256"], "tombstones": 1, "unknown_upper_bound": 128, "production_backend_authority": False}))
