import copy
import unittest
from audit_d04_timeline import audit


class TimelineAuditTests(unittest.TestCase):
    def fixture(self):
        def sample(at, target, used):
            return {"elapsed_nanoseconds": at, "state": {
                "snapshot_id": "scope", "budget_bytes": 100, "unissued_bytes": 100-used,
                "pending_grant_bytes": 0, "unknown_grant_bytes": 0, "available_bytes": 0,
                "held_bytes": 0, "used_bytes": used, "unknown_bytes": 0, "sessions": 0,
                "dynamic_target_bytes": target}}
        return {"dropped_periodic_samples": 0, "samples": [
            sample(0, 8<<20, 0), sample(1_000_000_000, 16<<20, 10),
            sample(70_000_000_000, 8<<20, 10)]}

    def test_idle_growth_and_shrink(self):
        self.assertTrue(audit(self.fixture(), 16<<20, True)["idle_shrink_checked"])

    def test_invalid_interior_sample_is_not_hidden_by_final_state(self):
        for field, value in (("held_bytes", 1), ("used_bytes", -1),
                             ("snapshot_id", "other"), ("dynamic_target_bytes", 32<<20)):
            data = self.fixture()
            data["samples"][1]["state"][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                audit(data, 16<<20, True)

    def test_idle_and_sampling_failures(self):
        data = self.fixture()
        variants = []
        x = copy.deepcopy(data); x["dropped_periodic_samples"] = 1; variants.append(x)
        x = copy.deepcopy(data); x["samples"][-1]["elapsed_nanoseconds"] = 2_000_000_000; variants.append(x)
        x = copy.deepcopy(data); x["samples"][-1]["state"]["dynamic_target_bytes"] = 16<<20; variants.append(x)
        x = copy.deepcopy(data); x["samples"][-1]["state"]["sessions"] = 1; variants.append(x)
        for item in variants:
            with self.assertRaises(ValueError):
                audit(item, 16<<20, True)


if __name__ == "__main__":
    unittest.main()
