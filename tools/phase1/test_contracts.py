import hashlib
import tempfile
import unittest
from pathlib import Path

from contracts import check_change, check_frozen


class ContractVersionTests(unittest.TestCase):
    def test_frozen_contract_accepts_approved_minor_version(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifact = root / "control.proto"
            artifact.write_bytes(b"approved 1.1.0 contract")
            digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
            versions = {"contracts": [{"id": "vortex.control.v1", "path": "control.proto", "version": "1.1.0", "status": "frozen"}]}
            review = {"status": "approved", "decision": "freeze-contracts", "contracts": [{"id": "vortex.control.v1", "version": "1.1.0", "sha256": digest, "decision": "approved"}]}
            self.assertEqual([], check_frozen(versions, review, root, {"vortex.control.v1"}))

    def test_frozen_contract_rejects_version_or_hash_drift(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifact = root / "control.proto"
            artifact.write_bytes(b"approved 1.1.0 contract")
            versions = {"contracts": [{"id": "vortex.control.v1", "path": "control.proto", "version": "1.2.0", "status": "frozen"}]}
            review = {"status": "approved", "decision": "freeze-contracts", "contracts": [{"id": "vortex.control.v1", "version": "1.1.0", "sha256": "stale", "decision": "approved"}]}
            errors = check_frozen(versions, review, root, {"vortex.control.v1"})
            self.assertTrue(any("version does not match" in error for error in errors))
            self.assertTrue(any("hash is missing or stale" in error for error in errors))

    def test_compatible_minor_change_requires_matching_change_record(self):
        before = {"vortex.control.v1": "1.0.0"}
        after = {"vortex.control.v1": "1.1.0"}
        good = {"changes": [{"contract_id": "vortex.control.v1", "version": "1.1.0", "kind": "compatible"}]}
        self.assertEqual([], check_change({"vortex.control.v1"}, before, after, good))
        bad = {"changes": [{"contract_id": "vortex.control.v1", "version": "1.1.0", "kind": "breaking"}]}
        self.assertTrue(check_change({"vortex.control.v1"}, before, after, bad))

    def test_unapproved_draft_version_is_not_semver_baseline(self):
        approved = {"vortex.control.v1": "1.0.0"}
        draft = {"vortex.control.v1": "1.1.0"}
        approved_change = {"changes": [{"contract_id": "vortex.control.v1", "version": "1.1.0", "kind": "compatible"}]}
        self.assertEqual([], check_change({"vortex.control.v1"}, approved, draft, approved_change))

    def test_approved_new_contract_can_start_at_one_zero_zero(self):
        after = {"vortex.authorization.v1": "1.0.0"}
        change = {"changes": [{"contract_id": "vortex.authorization.v1", "from": "none", "version": "1.0.0", "kind": "new"}]}
        self.assertEqual([], check_change({"vortex.authorization.v1"}, {}, after, change))


if __name__ == "__main__":
    unittest.main()
