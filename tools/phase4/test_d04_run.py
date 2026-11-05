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

    def test_percentile_reports_bucket_upper_bound(self):
        self.assertEqual(percentile_bucket([0, 1, 3, 0], .5), 7 / 1e6)
        self.assertEqual(percentile_bucket([1], .99), 1 / 1e6)
        with self.assertRaises(ValueError):
            percentile_bucket([0, 0], .5)


if __name__ == "__main__":
    unittest.main()
