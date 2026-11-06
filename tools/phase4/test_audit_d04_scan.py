import json
from pathlib import Path
import tempfile
import unittest

from audit_d04_scan import audit_accounting, audit_workload


class ByteReconciliationTests(unittest.TestCase):
    def fixture(self, directory):
        flows = {kind: {f'{"a"*32}/{i+1}/{network}': {'left_to_right': 1, 'right_to_left': 1}
                        for i in range(count)}
                 for kind, network, count in (('tcp', 1, 88), ('udp', 2, 2))}
        target = {'tcp_bytes_received': 88, 'tcp_bytes_sent': 88,
                  'udp_bytes_received': 2, 'udp_bytes_sent': 2}
        files = {'flow-accounting.json': flows,
                 'mixed-workload.json': {'tcp_streams': 16},
                 'report.json': {'classified_drops': {}, 'returned_reservations': {},
                                 'accounting': {'left_to_right': 90, 'right_to_left': 90}},
                 'events-source.jsonl.credit.json': {'used_bytes': 180},
                 'target-start.json': {key: 0 for key in target}, 'target-final.json': target}
        for name, value in files.items():
            (directory / name).write_text(json.dumps(value))

    def test_matches_independent_target_flow_and_source_totals(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            self.fixture(directory)
            self.assertEqual(audit_accounting(directory)['source_used_bytes'], 180)

    def test_corruption_cannot_hide_behind_report_pass(self):
        variants = [('target-final.json', 'tcp_bytes_received', 89),
                    ('events-source.jsonl.credit.json', 'used_bytes', 181),
                    ('target-final.json', 'udp_bytes_sent', -1)]
        for name, field, value in variants:
            with self.subTest(name=name, field=field), tempfile.TemporaryDirectory() as temp:
                directory = Path(temp)
                self.fixture(directory)
                path = directory / name
                data = json.loads(path.read_text()); data[field] = value
                path.write_text(json.dumps(data))
                with self.assertRaises(ValueError):
                    audit_accounting(directory)

    def test_missing_flow_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            self.fixture(directory)
            path = directory / 'flow-accounting.json'
            data = json.loads(path.read_text()); data['tcp'].pop(next(iter(data['tcp'])))
            path.write_text(json.dumps(data))
            with self.assertRaises(ValueError):
                audit_accounting(directory)


class WorkloadMetricTests(unittest.TestCase):
    def fixture(self):
        import copy
        tcp = dict(errors=[], connections=16, first_byte_latency_ms=[1.0]*16,
                   received_bytes=30_000_000, sent_bytes=30_000_000,
                   duration_seconds=15.0, throughput_mbps=16.0)
        return (dict(baseline=tcp, duration_seconds_per_phase=15.0,
                     mixed=dict(errors=[], tcp=copy.deepcopy(tcp), udp_sent=125000,
                                udp_received=125000, udp_unique_received=125000,
                                udp_bytes_received=150000000, udp_payload_mbps=80.0, udp_loss=0.0)),
                dict(samples_ms=[1.0]*50, median_ms=1.0, warmup_connections=5))

    def test_metrics_recompute(self):
        workload, short = self.fixture()
        self.assertTrue(audit_workload(workload, short)['udp_sequence_counts_checked'])

    def test_derived_metrics_cannot_override_raw_counts(self):
        mutations = [lambda w,s: w['mixed']['tcp'].update(throughput_mbps=17.0),
                     lambda w,s: w['mixed'].update(udp_loss=0.1),
                     lambda w,s: w['mixed'].update(udp_unique_received=125001),
                     lambda w,s: s.update(median_ms=2.0),
                     lambda w,s: s.update(samples_ms=[1.0]*49)]
        for mutate in mutations:
            workload, short = self.fixture()
            mutate(workload, short)
            with self.assertRaises(ValueError):
                audit_workload(workload, short)


if __name__ == '__main__':
    unittest.main()
