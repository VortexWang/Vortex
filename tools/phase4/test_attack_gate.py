import copy
import unittest
from attack_gate import EXPECTED, validate


class AttackGateTests(unittest.TestCase):
    def fixture(self):
        return dict(status='pass', cases={**{name: dict(status='pass', code=code, correlation_id='1'*32) for name, code in EXPECTED.items()}, 'udp-link-loss': dict(status='pass')})

    def test_complete_matrix(self):
        self.assertEqual(validate(self.fixture()), [])

    def test_each_case_is_required(self):
        for name in EXPECTED:
            report = self.fixture()
            del report['cases'][name]
            self.assertTrue(validate(report), name)

    def test_old_matrix_does_not_prove_stale_rejection(self):
        report = self.fixture()
        for protocol in ("tcp", "udp"):
            for case in ("stale-revision", "stale-generation"):
                del report["cases"][f"{protocol}/{case}"]
        self.assertEqual(len(validate(report)), 4)

    def test_top_level_pass_is_insufficient(self):
        self.assertTrue(validate(dict(status='pass')))
        self.assertTrue(validate(dict(status='pass', cases=[])))

    def test_wrong_codes_and_correlations_fail(self):
        for field, value in [('code', 2), ('code', True), ('correlation_id', '0'*32), ('correlation_id', None)]:
            report = self.fixture()
            report['cases']['tcp/future-revision'][field] = value
            self.assertTrue(validate(report), (field, value))

    def test_each_udp_failure_cannot_hide_behind_tcp(self):
        report = self.fixture()
        for name in list(report['cases']):
            if name.startswith('udp'):
                del report['cases'][name]
        self.assertEqual(len(validate(report)), 9)


if __name__ == '__main__':
    unittest.main()
