import unittest

from tools.phase1.contracts import check_change, check_map


class RequirementMapTests(unittest.TestCase):
    def test_rejects_missing_duplicate_unknown_and_invalid_contract_ids(self):
        source = "**ENG-001** and **NFR-001**"
        mapping = {"requirements":[
            {"id":"ENG-001", "contracts":["vortex.unknown"]},
            {"id":"ENG-001", "contracts":["vortex.database"]},
            {"id":"EXTRA-001", "contracts":["vortex.database"]},
        ]}
        errors = check_map(source, mapping)
        self.assertTrue(any("duplicate" in e for e in errors))
        self.assertTrue(any("NFR-001" in e for e in errors))
        self.assertTrue(any("invalid contract" in e for e in errors))
        self.assertTrue(any("EXTRA-001" in e for e in errors))

    def test_not_applicable_requires_reason(self):
        self.assertTrue(check_map("**NFR-001**", {"requirements":[{"id":"NFR-001", "decision":"not_applicable", "reason":""}]}))


class ChangeControlTests(unittest.TestCase):
    def test_compatible_change_requires_minor_and_record(self):
        before = {"vortex.database":"1.0.0"}
        after = {"vortex.database":"1.1.0"}
        self.assertFalse(check_change({"vortex.database"}, before, after, {"changes":[{"contract_id":"vortex.database", "version":"1.1.0", "kind":"compatible"}]}))
        self.assertTrue(check_change({"vortex.database"}, before, after, {"changes":[]}))
        self.assertTrue(check_change({"vortex.database"}, before, {"vortex.database":"1.0.1"}, {"changes":[{"contract_id":"vortex.database", "version":"1.0.1", "kind":"breaking"}]}))


if __name__ == "__main__":
    unittest.main()
