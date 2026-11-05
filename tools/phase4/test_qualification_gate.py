import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
GATE = ROOT / "tools/phase4/qualification_gate.py"


class QualificationGateTests(unittest.TestCase):
    def run_gate(self, runtime, tls, trial_reports=None, warmup=None):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            paths = {name: base / f"{name}.json" for name in ("runtime", "tls", "host", "output", "warmup")}
            paths["runtime"].write_text(json.dumps(runtime))
            paths["tls"].write_text(json.dumps(tls))
            paths["host"].write_text(json.dumps({"cpu_count": 8, "memory_gib": 16,
                "system": "Linux", "os_id": "ubuntu", "os_version": "24.04",
                "machine": "x86_64", "backend": "native", "driver_placement": "native-host",
                "qualification_interface": "eth0"}))
            trial_reports = trial_reports or [runtime]
            trial_paths = []
            for index, report in enumerate(trial_reports):
                path = base / f"trial-{index}.json"
                path.write_text(json.dumps(report))
                trial_paths.append(str(path))
            warmup = warmup or {"runtime_correctness": "pass", "authorization_mode": "runtime-authority",
                                "commit": runtime["commit"], "dirty": False,
                                "qualification_metrics": {"duration_seconds": 60}}
            paths["warmup"].write_text(json.dumps(warmup))
            command = [
                sys.executable, str(GATE), "--runtime", str(paths["runtime"]),
                "--tls", str(paths["tls"]), "--host", str(paths["host"]),
                "--trials", str(len(trial_paths)), "--trial-reports", *trial_paths,
                "--warmup-report", str(paths["warmup"]), "--warmup-seconds", "60",
                "--output", str(paths["output"]),
            ]
            result = subprocess.run(command, capture_output=True, text=True)
            return result.returncode, json.loads(paths["output"].read_text())

    def test_matching_clean_formal_evidence_passes(self):
        metrics = {"duration_seconds": 300, "tcp_streams": 16, "mixed_tcp_mbps": 500,
                   "udp_offered_mbps": 100, "udp_loss": 0.0001}
        runtime = {"performance_qualification": "pass", "authorization_mode": "runtime-authority",
                   "commit": "abc123", "semantic_manifest_sha256": "same", "dirty": False,
                   "trial_id": "trial-1", "qualification_metrics": metrics}
        tls = {"performance_qualification": "pass", "commit": "abc123",
               "semantic_manifest_sha256": "same", "dirty": False,
               "qualification_metrics": {"runtime_mixed_tcp_mbps": 500, "tls_mixed_tcp_mbps": 600,
                   "runtime_udp_offered_mbps": 100, "tls_udp_offered_mbps": 100,
                   "runtime_udp_loss": 0.0001, "tls_udp_loss": 0.0001, "same_workload_ratio": 0.83}}
        trials = [dict(runtime, trial_id=f"trial-{index}") for index in range(1, 4)]
        code, report = self.run_gate(runtime, tls, trials)
        self.assertEqual(code, 0)
        self.assertEqual(report["status"], "pass")

    def test_mismatched_or_dirty_evidence_fails_closed(self):
        runtime = {"performance_qualification": "pass", "runtime_correctness": "pass",
                   "authorization_mode": "runtime-authority", "commit": "abc123",
                   "semantic_manifest_sha256": "runtime", "dirty": False,
                   "trial_id": "trial-1", "qualification_metrics": {}}
        tls = {"performance_qualification": "pass", "commit": "def456",
               "semantic_manifest_sha256": "tls", "dirty": True}
        trials = [dict(runtime, trial_id=f"trial-{index}") for index in range(1, 4)]
        code, report = self.run_gate(runtime, tls, trials)
        self.assertNotEqual(code, 0)
        self.assertEqual(report["status"], "not-qualified")
        self.assertIn("runtime and independent TLS commit identity differs", report["reasons"])
        self.assertIn("runtime and independent TLS workload identity differs", report["reasons"])
        self.assertIn("qualification evidence was collected from a dirty worktree", report["reasons"])


if __name__ == "__main__":
    unittest.main()
