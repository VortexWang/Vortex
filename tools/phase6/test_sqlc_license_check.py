"""CR-003 authorization must not survive artifact, use or dependency drift."""
import copy
import json
from pathlib import Path
import unittest

from sqlc_license_check import exception_matches

ROOT = Path(__file__).resolve().parents[2]


class ExactToolLicenseException(unittest.TestCase):
    def setUp(self):
        self.approval = json.loads((ROOT / "docs/governance/tool-license-exceptions.json").read_text())["exceptions"][0]

    def matches(self, artifact=None, dependency=None, usage=None, license_id="MPL-2.0", approval=None):
        return exception_matches(approval or self.approval,
                                 artifact or self.approval["artifacts"][0],
                                 usage or self.approval["usage"],
                                 dependency or self.approval["dependency"], license_id)

    def test_exact_three_official_artifacts(self):
        self.assertEqual(len(self.approval["artifacts"]), 3)
        for artifact in self.approval["artifacts"]:
            self.assertTrue(self.matches(artifact=artifact))

    def test_artifact_platform_origin_archive_and_binary_drift(self):
        for key in ("platform", "url", "archive_sha256", "binary_sha256"):
            artifact = dict(self.approval["artifacts"][0])
            artifact[key] += "-changed"
            with self.subTest(key=key):
                self.assertFalse(self.matches(artifact=artifact))

    def test_dependency_name_version_and_sum_drift(self):
        for key in ("module", "version", "sum"):
            dependency = dict(self.approval["dependency"])
            dependency[key] += "-changed"
            with self.subTest(key=key):
                self.assertFalse(self.matches(dependency=dependency))

    def test_product_linkage_and_distribution_are_not_authorized(self):
        for usage in ("product-runtime", "distributed-tool-image", "development-ci-sql-to-go-modified"):
            self.assertFalse(self.matches(usage=usage))

    def test_other_licenses_and_pending_decisions_are_not_authorized(self):
        for license_id in ("MIT-0", "GPL-3.0-only", "MPL-2.0-or-later"):
            self.assertFalse(self.matches(license_id=license_id))
        approval = copy.deepcopy(self.approval)
        approval["status"] = "pending"
        self.assertFalse(self.matches(approval=approval))
        approval["status"] = "approved"
        approval["decision"] = "B"
        self.assertFalse(self.matches(approval=approval))


if __name__ == "__main__":
    unittest.main()
