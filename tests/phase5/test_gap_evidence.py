import gzip
import hashlib
import json
import sqlite3
import tempfile
import unittest
from pathlib import Path

from tools.phase5.audit_gap_evidence import PROCESS_CASES, validate_manifest


class GapEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.sha = "a" * 40
        db = self.root / "ledger.db"
        con = sqlite3.connect(db)
        con.execute("CREATE TABLE proof (value INTEGER)")
        con.execute("INSERT INTO proof VALUES (1)")
        con.commit()
        con.close()
        self.raw = db.read_bytes()
        self.snapshot = self.root / "policy/snapshot.sqlite.gz"
        self.snapshot.parent.mkdir()
        self.snapshot.write_bytes(gzip.compress(self.raw))
        self.manifest = {
            "schema": 1,
            "status": "passed",
            "source_sha": self.sha,
            "source_clean": True,
            "host": {"go_version": "go1.27.3"},
            "backend": "protocol-test-only",
            "production_backend_authority": False,
            "process_cases": [],
            "audits": {name: name for name in ("audit.json", "policy-audit.json", "renewal-safety-audit.json", "retention-audit.json", "recovery/audit.json")},
            "snapshots": [],
            "files": [],
        }
        for name, payload in (("report.json", {"commit": self.sha, "status": "passed"}), ("recovery/host.txt", "host fixture")):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(json.dumps(payload) if isinstance(payload, dict) else payload)
            self.manifest["files"].append({"path": name, "sha256": self.digest(path)})
        for audit in self.manifest["audits"].values():
            path = self.root / audit
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('{"status":"passed"}')
            self.manifest["files"].append({"path": audit, "sha256": self.digest(path), "size": path.stat().st_size})
        for log_name, names in PROCESS_CASES.items():
            path = self.root / log_name
            path.write_text("".join(f"--- PASS: {name} (0.00s)\n" for name in names))
            self.manifest["files"].append({"path": log_name, "sha256": self.digest(path), "size": path.stat().st_size})
            self.manifest["process_cases"].extend({"name": name, "log": log_name, "status": "passed"} for name in names)
        snapshot_meta = {"path": "policy/snapshot.sqlite.gz", "sha256": self.digest(self.snapshot), "raw_sha256": hashlib.sha256(self.raw).hexdigest()}
        self.manifest["snapshots"].append(snapshot_meta)
        self.manifest["files"].append({"path": snapshot_meta["path"], "sha256": snapshot_meta["sha256"], "size": self.snapshot.stat().st_size})
        recovery = self.root / "recovery/recovery-hub.json"
        recovery.parent.mkdir(parents=True, exist_ok=True)
        recovery.write_text('{"Info":{"JournalMode":"wal","Synchronous":"2"}}')
        self.manifest["files"].append({"path": "recovery/recovery-hub.json", "sha256": self.digest(recovery), "size": recovery.stat().st_size})

    def tearDown(self):
        self.temp.cleanup()

    @staticmethod
    def digest(path):
        return hashlib.sha256(path.read_bytes()).hexdigest()

    def rejected(self, manifest=None, sha=None):
        with self.assertRaises((ValueError, KeyError, OSError, json.JSONDecodeError, gzip.BadGzipFile)):
            validate_manifest(self.root, manifest or self.manifest, sha or self.sha)

    def test_complete_evidence_passes(self):
        validate_manifest(self.root, self.manifest, self.sha)

    def test_missing_evidence_report_fails(self):
        (self.root / "policy-audit.json").unlink()
        self.rejected()

    def test_wrong_source_sha_fails(self):
        self.rejected(sha="b" * 40)

    def test_changed_file_hash_fails(self):
        (self.root / "audit.json").write_text('{"status":"passed","changed":true}')
        self.rejected()

    def test_unhashed_process_log_fails(self):
        self.manifest["files"] = [entry for entry in self.manifest["files"] if entry["path"] != "policy-process.log"]
        self.rejected()

    def test_omitted_snapshot_record_fails(self):
        self.manifest["snapshots"] = []
        self.rejected()

    def test_skipped_process_scenario_fails(self):
        path = self.root / "policy-process.log"
        path.write_text(path.read_text().replace("--- PASS: TestSharePolicyProcess/policy-cutover", "--- SKIP: TestSharePolicyProcess/policy-cutover"))
        self.update_file("policy-process.log")
        self.rejected()

    def test_empty_process_log_fails(self):
        (self.root / "regional-process.log").write_text("")
        self.update_file("regional-process.log")
        self.rejected()

    def test_forged_snapshot_fails(self):
        self.snapshot.write_bytes(gzip.compress(b"not sqlite"))
        self.update_file("policy/snapshot.sqlite.gz")
        self.rejected()

    def test_partially_collected_case_set_fails(self):
        self.manifest["process_cases"].pop()
        self.rejected()

    def update_file(self, name):
        path = self.root / name
        for entry in self.manifest["files"]:
            if entry["path"] == name:
                entry["sha256"] = self.digest(path)
                entry["size"] = path.stat().st_size
                break


if __name__ == "__main__":
    unittest.main()
