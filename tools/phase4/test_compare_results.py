import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("compare_results.py")


class ComparisonTests(unittest.TestCase):
    def evaluate(self, loss, formal=True):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            runtime, output = root / "runtime", root / "comparison"
            runtime.mkdir()
            output.mkdir()
            host = {"commit": "tested-sha", "dirty": False,
                    "semantic_manifest_sha256": "same-workload",
                    "runtime_environment": {}, "authorization_mode": "runtime-authority"}
            workload = {"duration_seconds_per_phase": 300, "tcp_streams": 16,
                        "udp_target_mbps": 100, "baseline": {"throughput_mbps": 800},
                        "mixed": {"errors": [], "tcp": {"errors": [], "throughput_mbps": 750},
                                  "udp_sent": 3125000, "udp_loss": 0}}
            files = {runtime / "host.json": host, runtime / "mixed-workload.json": workload,
                     output / "host.json": host, output / "runtime-host.json": host,
                     output / "tls-throughput.json": {"throughput_mbps": 1000},
                     output / "runtime-latency.json": {"median_ms": 2},
                     output / "tls-latency.json": {"median_ms": 1}}
            tls = json.loads(json.dumps(workload))
            tls["mixed"]["tcp"]["throughput_mbps"] = 1000
            if loss is None:
                del tls["mixed"]["udp_loss"]
            else:
                tls["mixed"]["udp_loss"] = loss
            files[output / "tls-mixed-throughput.json"] = tls
            for name in ("forged-open", "binary-integrity", "middle-hop-fault",
                         "bypass-relay-2", "bypass-destination"):
                files[output / f"{name}.json"] = {"status": "pass"}
            for index in range(5):
                files[output / f"rtt-{index}.json"] = {"median_ms": 0.1}
            for path, value in files.items():
                path.write_text(json.dumps(value))
            result = subprocess.run([sys.executable, str(SCRIPT), str(runtime), str(output)],
                                    env={**os.environ, "PHASE4_FORMAL": "1" if formal else "0"},
                                    capture_output=True, text=True)
            if result.returncode:
                return result.returncode, None
            return 0, json.loads((output / "comparison.json").read_text())

    def test_nested_zero_loss_passes(self):
        code, report = self.evaluate(0)
        self.assertEqual(code, 0)
        self.assertEqual(report["performance_qualification"], "pass")
        self.assertEqual(report["commit"], "tested-sha")
        self.assertEqual(len(report["report_evaluator_sha256"]), 64)

    def test_excess_loss_remains_unqualified(self):
        code, report = self.evaluate(0.002)
        self.assertEqual(code, 0)
        self.assertEqual(report["performance_qualification"], "not-qualified")

    def test_missing_loss_fails_closed(self):
        code, _ = self.evaluate(None)
        self.assertNotEqual(code, 0)

    def test_diagnostic_does_not_qualify(self):
        _, report = self.evaluate(0, formal=False)
        self.assertEqual(report["performance_qualification"], "not-qualified")


if __name__ == "__main__":
    unittest.main()
