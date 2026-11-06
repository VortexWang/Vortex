"""Exact CR-003/004 authorization must not survive artifact/source/use drift."""
import copy
import json
from pathlib import Path
import unittest

from sqlc_license_check import exception_matches, embedded_exception_matches
from compare_wasm_archive import normalized, ar_members
import tempfile

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


class ExactEmbeddedLicenseException(unittest.TestCase):
    def setUp(self):
        approvals = json.loads((ROOT / "docs/governance/tool-license-exceptions.json").read_text())["exceptions"]
        self.approval = next(row for row in approvals if row["id"] == "CR-004")
        self.review = json.loads((ROOT / "tools/phase6/sqlc-dependencies.json").read_text())

    def matches(self, item, *, artifact=None, source=None, license_id=None, usage=None, approval=None):
        return embedded_exception_matches(
            self.approval if approval is None else approval,
            self.approval["artifacts"][0] if artifact is None else artifact,
            self.approval["usage"] if usage is None else usage,
            item["source"] if source is None else source,
            item["license"] if license_id is None else license_id)

    def test_both_reviewed_sources_match_only_three_official_artifacts(self):
        self.assertEqual(self.approval["owner_response"], "a")
        self.assertEqual(self.approval["artifacts"], self.review["artifacts"])
        self.assertEqual(len(self.approval["embedded_sources"]), 2)
        for item, source in zip(self.approval["embedded_sources"], self.review["embedded_sources"]):
            for artifact in self.review["artifacts"]:
                self.assertTrue(self.matches(item, source=source, artifact=artifact))

    def test_module_version_sum_component_asset_and_file_digest_drift(self):
        for item in self.approval["embedded_sources"]:
            for key in ("module", "version", "sum", "component", "binary_asset"):
                source = copy.deepcopy(item["source"])
                source[key] += "-changed"
                with self.subTest(module=source["module"], key=key):
                    self.assertFalse(self.matches(item, source=source))
            for name in item["source"]["files"]:
                source = copy.deepcopy(item["source"])
                source["files"][name] = "0" * 64
                self.assertFalse(self.matches(item, source=source))
                del source["files"][name]
                self.assertFalse(self.matches(item, source=source))
            source = copy.deepcopy(item["source"])
            source["files"]["unreviewed.c"] = "0" * 64
            self.assertFalse(self.matches(item, source=source))

    def test_missing_identity_and_artifact_fields_are_rejected(self):
        for item in self.approval["embedded_sources"]:
            for key in item["source"]:
                source = copy.deepcopy(item["source"])
                del source[key]
                self.assertFalse(self.matches(item, source=source))
            for key in self.approval["artifacts"][0]:
                artifact = dict(self.approval["artifacts"][0])
                artifact[key] += "-changed"
                self.assertFalse(self.matches(item, artifact=artifact))
                del artifact[key]
                self.assertFalse(self.matches(item, artifact=artifact))

    def test_unapproved_ordinary_gpl_missing_or_different_bison_exception(self):
        for item in self.approval["embedded_sources"]:
            for license_id in ("GPL-2.0-or-later", "GPL-2.0-only", "GPL-3.0-only",
                               "GPL-2.0-or-later WITH Bison-exception-2.1", "UNKNOWN"):
                self.assertFalse(self.matches(item, license_id=license_id))
        allocator, parser = self.approval["embedded_sources"]
        self.assertFalse(self.matches(allocator, license_id=parser["license"]))
        self.assertFalse(self.matches(parser, license_id=allocator["license"]))

    def test_missing_pending_or_different_approval_is_rejected(self):
        for item in self.approval["embedded_sources"]:
            self.assertFalse(self.matches(item, approval={}))
            for key, value in (("id", "CR-003"), ("status", "pending"), ("decision", "B"),
                               ("embedded_sources", []), ("artifacts", []), ("usage", "product-runtime")):
                approval = copy.deepcopy(self.approval)
                approval[key] = value
                self.assertFalse(self.matches(item, approval=approval))

    def test_modified_distribution_runtime_and_skeleton_uses_are_rejected(self):
        for item in self.approval["embedded_sources"]:
            for usage in ("product-runtime", "distributed-tool-image", "parser-generator-skeleton",
                          "development-ci-sql-to-go-modified", ""):
                self.assertFalse(self.matches(item, usage=usage))

    def test_approvals_do_not_widen_global_policy(self):
        allowed = json.loads((ROOT / "docs/governance/license-policy.yaml").read_text())["allowed_spdx"]
        for item in self.approval["embedded_sources"]:
            self.assertNotIn(item["license"], allowed)
        self.assertNotIn("GPL-2.0-or-later", allowed)


class WasmSourceComparison(unittest.TestCase):
    @staticmethod
    def module(code=b"one", producer=b"clang-A", extra=b""):
        name = b"\x09producers"
        return b"\0asm\x01\0\0\0" + bytes([10, len(code)]) + code + bytes([0, len(name + producer)]) + name + producer + extra

    def test_only_compiler_producer_string_may_differ(self):
        self.assertEqual(normalized(self.module(producer=b"clang-A")), normalized(self.module(producer=b"clang-B")))
        self.assertNotEqual(normalized(self.module(code=b"one")), normalized(self.module(code=b"two")))

    def test_every_other_section_and_section_order_remains_significant(self):
        for section_id in (1, 2, 3, 11, 12):
            extra = bytes([section_id, 1, 0])
            self.assertNotEqual(normalized(self.module()), normalized(self.module(extra=extra)))
        for name in (b"linking", b"reloc.CODE", b"target_features"):
            payload = bytes([len(name)]) + name
            extra = bytes([0, len(payload)]) + payload
            self.assertNotEqual(normalized(self.module()), normalized(self.module(extra=extra)))

    def test_archive_duplicate_members_are_preserved(self):
        def member(payload):
            header = f"{'same.o/':<16}{0:<12}{0:<6}{0:<6}{644:<8}{len(payload):<10}`\n".encode()
            return header + payload + (b"\n" if len(payload) % 2 else b"")
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "lib.a"
            path.write_bytes(b"!<arch>\n" + member(b"one") + member(b"two"))
            self.assertEqual(list(ar_members(path)), [("same.o", b"one"), ("same.o", b"two")])


if __name__ == "__main__":
    unittest.main()
