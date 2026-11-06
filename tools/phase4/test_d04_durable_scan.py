import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import d04_durable_scan


class DurableScanTests(unittest.TestCase):
    def test_case_summary_requires_both_networks_and_settled_fields(self):
        with tempfile.TemporaryDirectory() as root:
            output = Path(root) / "case"
            item = {
                "source_conserved": True,
                "source_settled": True,
                "pending_unknown_held_zero": True,
                "issuer_conserved": True,
                "issuer_occupancy_bytes": 0,
                "idle_tail_reclaimed": True,
                "pending_demand_settled": True,
                "pending_usage_settled": True,
            }
            completed = type("Completed", (), {"returncode": 0, "stdout": "ok\n", "stderr": ""})()
            def fake_run(command, cwd, env, text, capture_output, check):
                result_dir = Path(env["VORTEX_D04_RESULT_DIR"])
                result_dir.mkdir(parents=True)
                (result_dir / "network-1.json").write_text(json.dumps(item))
                (result_dir / "network-2.json").write_text(json.dumps(item))
                return completed
            with patch.object(d04_durable_scan.subprocess, "run", side_effect=fake_run):
                summary = d04_durable_scan.run_case(
                    {"id": "case", "block_bytes": 65536, "target_seconds": 10, "renew_percent": 20},
                    output,
                    "go",
                )
            self.assertEqual(summary["status"], "pass")
            self.assertTrue(summary["checks"]["durable_conservation_and_settlement"])

    def test_case_summary_fails_when_udp_evidence_is_missing(self):
        with tempfile.TemporaryDirectory() as root:
            output = Path(root) / "case"
            completed = type("Completed", (), {"returncode": 0, "stdout": "ok\n", "stderr": ""})()
            def fake_run(command, cwd, env, text, capture_output, check):
                result_dir = Path(env["VORTEX_D04_RESULT_DIR"])
                result_dir.mkdir(parents=True)
                (result_dir / "network-1.json").write_text(json.dumps({"source_conserved": True}))
                return completed
            with patch.object(d04_durable_scan.subprocess, "run", side_effect=fake_run):
                summary = d04_durable_scan.run_case(
                    {"id": "case", "block_bytes": 65536, "target_seconds": 10, "renew_percent": 20},
                    output,
                    "go",
                )
            self.assertEqual(summary["status"], "fail")
            self.assertFalse(summary["checks"]["both_networks_present"])


if __name__ == "__main__":
    unittest.main()
