import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

from tools.phase1.baseline import check_candidates, check_evidence, check_lock, main


class CandidateChecks(unittest.TestCase):
    def test_rejects_unverified_or_disallowed_candidate(self):
        item = {"name":"x", "version":"1.0", "source":"https://example.test", "license":"AGPL-3.0", "license_evidence":"https://example.test/license", "integrity":"sha256:" + "a" * 64, "status":"verified-candidate"}
        self.assertTrue(any("disallowed" in e for e in check_candidates({"candidates":[item]})))

    def test_rejects_placeholder_integrity(self):
        item = {"name":"x", "version":"1.0", "source":"https://example.test", "license":"MIT", "license_evidence":"https://example.test/license", "integrity":"pending", "status":"candidate"}
        errors = check_candidates({"candidates":[item]})
        self.assertTrue(any("immutable checksum" in e for e in errors))


class EvidenceChecks(unittest.TestCase):
    def test_missing_original_remains_blocked(self):
        with tempfile.TemporaryDirectory() as temp:
            register = {"items":[{"artifact":"original.md", "status":"missing", "evidence_link":None}]}
            self.assertEqual("blocked", check_evidence(register, Path(temp))[0])

    def test_owner_restart_disposition_is_nonblocking_but_not_passed(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            decision = root / "decision.md"
            decision.write_text("owner decision", encoding="utf-8")
            register = {"status":"out-of-scope", "decision":"decision.md", "items":[{"artifact":"original.md", "status":"missing", "evidence_link":None}]}
            self.assertEqual(("out-of-scope", []), check_evidence(register, root))

    def test_out_of_scope_requires_decision_and_preserves_missing_rows(self):
        with tempfile.TemporaryDirectory() as temp:
            register = {"status":"out-of-scope", "decision":"missing.md", "items":[{"artifact":"original.md", "status":"verified", "evidence_link":"x"}]}
            status, errors = check_evidence(register, Path(temp))
            self.assertEqual("blocked", status)
            self.assertEqual(3, len(errors))

    def test_verified_requires_authentic_record_metadata(self):
        with tempfile.TemporaryDirectory() as temp:
            Path(temp, "original.md").write_text("source", encoding="utf-8")
            register = {"items":[{"artifact":"original.md", "status":"verified", "evidence_link":"source.md"}]}
            self.assertTrue(check_evidence(register, Path(temp))[1])

    def test_missing_register_is_reported_as_blocked(self):
        with tempfile.TemporaryDirectory() as temp, redirect_stdout(StringIO()):
            missing = Path(temp, "evidence-register.yaml")
            with patch("sys.argv", ["baseline.py", "evidence", "--register", str(missing)]):
                self.assertEqual(1, main())


class LockChecks(unittest.TestCase):
    def test_accepts_locked_subset_with_matching_approved_digest(self):
        candidate = {"name":"x", "version":"1.0", "source":"https://example.test/x/1.0", "license":"MIT", "license_evidence":"https://example.test/license", "integrity":"sha256:" + "a" * 64, "status":"verified-candidate"}
        manifest = {"approval":{"manifest_sha256":"d" * 64}, "candidates":[candidate]}
        lock = {"candidate_manifest_sha256":"d" * 64, "user_decision":"approve-candidates", "locked_candidates":[dict(candidate, status="locked")], "not_locked":[]}
        self.assertEqual([], check_lock(lock, manifest, {"allowed_spdx":["MIT"]}))

    def test_rejects_digest_drift_and_floating_source(self):
        candidate = {"name":"x", "version":"1.0", "source":"https://example.test/x/latest", "license":"MIT", "license_evidence":"https://example.test/license", "integrity":"sha256:" + "a" * 64, "status":"verified-candidate"}
        manifest = {"approval":{"manifest_sha256":"d" * 64}, "candidates":[candidate]}
        lock = {"candidate_manifest_sha256":"e" * 64, "user_decision":"approve-candidates", "locked_candidates":[dict(candidate, status="locked")], "not_locked":[]}
        errors = check_lock(lock, manifest, {"allowed_spdx":["MIT"]})
        self.assertTrue(any("digest" in e for e in errors))
        self.assertTrue(any("floating" in e for e in errors))

    def test_default_license_policy_is_explicit_allowlist(self):
        candidate = {"name":"x", "version":"1.0", "source":"https://example.test/x/1.0", "license":"MIT", "license_evidence":"https://example.test/license", "integrity":"sha256:" + "a" * 64, "status":"verified-candidate"}
        manifest = {"approval":{"manifest_sha256":"d" * 64}, "candidates":[candidate]}
        lock = {"candidate_manifest_sha256":"d" * 64, "user_decision":"approve-candidates", "locked_candidates":[dict(candidate, status="locked")], "not_locked":[]}
        self.assertEqual([], check_lock(lock, manifest))


if __name__ == "__main__":
    unittest.main()
