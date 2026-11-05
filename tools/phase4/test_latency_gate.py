import json
import tempfile
import unittest
from pathlib import Path
from latency_gate import qualify


class LatencyGateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        for mode in ("pool", "mux"):
            out = self.base / mode / "comparison"
            out.mkdir(parents=True)
            self.write(mode, "runtime-host.json", dict(dirty=False, authorization_mode="runtime-authority", backend="native", system="Linux", commit="test-sha"))
            self.write(mode, "config/source.json", dict(transport_mode=mode))
            self.write(mode, "runtime-latency.json", dict(samples_ms=[7.0] * 50))
            self.write(mode, "comparison.json", dict(commit="test-sha", latency=dict(runtime_minus_segment_sum_ms=2.0), binary_integrity=dict(status="pass"), forged_open=dict(status="pass"), middle_hop_failure=dict(status="pass"), direct_bypass={"relay-2": dict(status="pass"), "destination": dict(status="pass")}))
            for name in ("source", "relay-1", "relay-2", "relay-3", "destination"):
                self.write(mode, f"rtt-{name}.json", dict(samples_ms=[1.0] * 50, median_ms=1.0))

    def write(self, mode, name, value):
        path = self.base / mode / "comparison" / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(value))

    def test_complete_consistent_modes_pass(self):
        self.assertEqual(qualify(self.base)["status"], "pass")

    def test_missing_mode_fails(self):
        (self.base / "pool/comparison/runtime-host.json").unlink()
        self.assertEqual(qualify(self.base)["status"], "not-qualified")

    def test_edited_aggregate_cannot_replace_raw_samples(self):
        self.write("mux", "runtime-latency.json", dict(samples_ms=[12.0] * 50))
        self.assertEqual(qualify(self.base)["status"], "not-qualified")

    def test_nonfinite_sample_fails(self):
        self.write("mux", "runtime-latency.json", dict(samples_ms=[float("nan")] * 50))
        self.assertEqual(qualify(self.base)["status"], "not-qualified")

    def test_diagnostic_authority_fails(self):
        self.write("pool", "runtime-host.json", dict(dirty=False, authorization_mode="qualification-only", backend="native", system="Linux", commit="test-sha"))
        self.assertEqual(qualify(self.base)["status"], "not-qualified")


if __name__ == "__main__":
    unittest.main()
