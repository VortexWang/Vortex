import unittest
from d04_run import candidates, percentile_bucket


class D04Tests(unittest.TestCase):
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


if __name__ == "__main__":
    unittest.main()
