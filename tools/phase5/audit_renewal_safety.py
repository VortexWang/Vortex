#!/usr/bin/env python3
"""Independent inspection of finite renewal/race fixtures; no production claims."""
import gzip
import hashlib
import json
from pathlib import Path
import sqlite3
import sys
import tempfile


def fields(raw):
    """Read the approved flat canonical protobuf fields (varint/bytes only)."""
    offset = 0
    result = {}
    segments = []

    def number():
        nonlocal offset
        value = 0
        for shift in range(0, 70, 7):
            assert offset < len(raw)
            byte = raw[offset]
            offset += 1
            value |= (byte & 127) << shift
            if not byte & 128:
                return value
        raise AssertionError("overlong varint")

    while offset < len(raw):
        start = offset
        key = number()
        field, wire = key >> 3, key & 7
        assert field > 0 and field not in result
        if wire == 0:
            value = number()
        else:
            assert wire == 2
            size = number()
            assert offset + size <= len(raw)
            value = raw[offset:offset + size]
            offset += size
        result[field] = value
        segments.append((field, raw[start:offset]))
    assert [x[0] for x in segments] == sorted(result)
    return result, segments


def checked_request(raw, expected):
    decoded, segments = fields(raw)
    assert decoded[13] == expected
    unsigned = b"".join(segment for tag, segment in segments if tag != 13)
    assert hashlib.sha256(b"vortex.renewal.v1\0" + unsigned).digest() == expected
    return decoded


out = Path(sys.argv[1])
labels = ["revoke-before-result", "revoke-after-result", "revoke-concurrent",
          "time-exact-expiry", "time-stale-anchor", "late-session"]
results = []
for label in labels:
    prefix = "renewal-safety-" + label
    hashes = json.loads((out / (prefix + "-ledgers.json")).read_text())
    assert set(hashes) == {"agent", "hub"}
    with tempfile.TemporaryDirectory() as directory:
        dbs = {}
        for role in hashes:
            raw = gzip.decompress((out / (prefix + f"-{role}.sqlite.gz")).read_bytes())
            assert hashlib.sha256(raw).hexdigest() == hashes[role]
            path = Path(directory) / (role + ".db")
            path.write_bytes(raw)
            dbs[role] = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
            assert dbs[role].execute("PRAGMA integrity_check").fetchone() == ("ok",)
            assert dbs[role].execute("PRAGMA journal_mode").fetchone() == ("wal",)
        agent, hub = dbs["agent"], dbs["hub"]
        request, request_hash, state, result, result_hash = agent.execute("SELECT payload,payload_sha256,state,result,result_sha256 FROM renewal_requests").fetchone()
        decoded = checked_request(request, request_hash)
        stored_request, stored_hash, stored_result, stored_result_hash = hub.execute("SELECT request,request_sha256,result,result_sha256 FROM issuer_renewals").fetchone()
        assert stored_request == request and stored_hash == request_hash
        assert hashlib.sha256(stored_result).digest() == stored_result_hash
        response, _ = fields(stored_result)
        assert response[1] == decoded[1] and response[2] == request_hash and response[3] == b"ok"
        if label == "late-session":
            assert state == "pending" and not result and not result_hash
        else:
            assert state in {"received", "installed"}
            assert result == stored_result and result_hash == stored_result_hash
        initial, remaining = agent.execute("SELECT SUM(CAST(initial_bytes AS INTEGER)),SUM(CAST(remaining_bytes AS INTEGER)) FROM authorization_balances").fetchone()
        usage = agent.execute("SELECT state,used_bytes FROM usage_reservations").fetchall()
        assert sorted(usage) == [("committed", "64"), ("unknown", "128")]
        assert initial == remaining + 64 + 128
        assert agent.execute("SELECT unknown_upper_bound,liability_state FROM unknown_usage_batches").fetchall() == [("128", "unresolved")]
        if label.startswith("revoke-"):
            assert agent.execute("SELECT DISTINCT state FROM authorization_records").fetchall() == [("revoked",)]
            assert agent.execute("SELECT COUNT(*) FROM authorization_revocations").fetchone()[0] == 6
            assert initial == (8192 if state == "installed" else 4096)
            if label == "revoke-before-result":
                assert state == "received"
            if label == "revoke-after-result":
                assert state == "installed"
        else:
            assert initial == (4096 if label == "late-session" else 8192)
        assert hub.execute("SELECT remaining_bytes FROM issuer_delegations").fetchone() == ("8192",)
        assert hub.execute("SELECT COUNT(*) FROM issuer_issues").fetchone()[0] == 2
        for db in (agent, hub):
            for table in ("authorization_updates", "unknown_usage_batches", "usage_batches"):
                for payload, digest in db.execute(f"SELECT payload,payload_sha256 FROM {table}"):
                    assert hashlib.sha256(payload).digest() == digest
            db.close()
        results.append({"case": label, "agent_issued": initial, "agent_remaining": remaining,
                        "committed": 64, "unknown": 128, "hub_issued": 8192, "state": state})
print(json.dumps({"schema": 1, "status": "passed", "scope": "finite-renewal-safety", "snapshots": 12,
                  "production_backend_authority": False, "cases": results}))
