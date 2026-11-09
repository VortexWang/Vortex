import unittest
from d04_run import candidates, percentile_bucket, summarize_observations


class D04Tests(unittest.TestCase):
    def test_full_factorial_has_all_unique_combinations(self):
        cases = candidates(100, full_factorial=True)
        combinations = {(c["block_bytes"], c["grant_target_seconds"], c["waterline_percent"]) for c in cases}
        self.assertEqual(len(cases), 27)
        self.assertEqual(len(combinations), 27)
        self.assertEqual(len({c["id"] for c in cases}), 27)
        for c in cases:
            self.assertEqual(c["grant_target_bytes"], 12500000 * c["grant_target_seconds"])

    def test_one_factor_changes_and_fixed_rate_conversion(self):
        cases = candidates(100)
        self.assertEqual(len(cases), 7)
        fields = ("block_bytes", "grant_target_seconds", "waterline_percent")
        for case in cases[1:]:
            self.assertEqual(sum(case[k] != cases[0][k] for k in fields), 1)
        for case in cases:
            self.assertEqual(case["grant_target_bytes"], 12500000 * case["grant_target_seconds"])
        self.assertTrue(all(c["grant_target_bytes"] >= 8 * 1024 * 1024 for c in candidates(0.001)))

    def test_target_seconds_is_the_dynamic_target_factor(self):
        cases = candidates(100)
        self.assertEqual(cases[0]["grant_target_seconds"], 30)
        self.assertEqual(cases[3]["grant_target_seconds"], 10)
        self.assertEqual(cases[4]["grant_target_seconds"], 60)

    def test_invalid_histogram_cannot_produce_review_evidence(self):
        for buckets in ([], {}, [True], [-1, 2], [1.5], [float("nan")], [0] * 65):
            with self.subTest(buckets=buckets), self.assertRaises(ValueError):
                percentile_bucket(buckets, .99)
        for percentile in (0, -1, 1.1, True, float("nan"), float("inf")):
            with self.subTest(percentile=percentile), self.assertRaises(ValueError):
                percentile_bucket([1], percentile)

    def test_percentile_reports_bucket_upper_bound(self):
        self.assertEqual(percentile_bucket([0, 1, 3, 0], .5), 7 / 1e6)
        self.assertEqual(percentile_bucket([1], .99), 1 / 1e6)
        with self.assertRaises(ValueError):
            percentile_bucket([0, 0], .5)

    def test_observation_summary_reports_merge_and_occupancy(self):
        result = summarize_observations({
            "reservations": 12,
            "block_allocations": 3,
            "renewal_attempts": 10,
            "renewal_merged": 7,
            "idle_reclaims": 2,
            "idle_reclaimed_bytes": 128,
            "peak_sessions": 4,
            "peak_held_bytes": 512,
            "peak_pending_grant_bytes": 256,
            "peak_unknown_grant_bytes": 64,
            "issuer_calls": 5,
        })
        self.assertEqual(result["renewal_merged"], 7)
        self.assertEqual(result["peak_pending_grant_bytes"], 256)
        self.assertEqual(result["idle_reclaimed_bytes"], 128)
        self.assertAlmostEqual(result["renewal_merge_rate"], .7)

    def test_observation_summary_rejects_invalid_counts(self):
        values = {
            "reservations": 1,
            "block_allocations": 1,
            "renewal_attempts": 1,
            "renewal_merged": 1,
            "idle_reclaims": 0,
            "idle_reclaimed_bytes": 0,
            "peak_sessions": 1,
            "peak_held_bytes": 1,
            "peak_pending_grant_bytes": 0,
            "peak_unknown_grant_bytes": 0,
            "issuer_calls": 1,
        }
        values["renewal_merged"] = 2
        with self.assertRaises(ValueError):
            summarize_observations(values)
        values["renewal_merged"] = 0
        values["block_allocations"] = 0
        with self.assertRaises(ValueError):
            summarize_observations(values)


if __name__ == "__main__":
    unittest.main()
