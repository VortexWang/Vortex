"""Supported reporter for the real PostgreSQL stale observation regression."""
from pathlib import Path
import subprocess
import unittest


class SupersededRuleObservation(unittest.TestCase):
    def test_no_fresh_commit(self):
        result = subprocess.run(
            ["go", "test", "./internal/service", "-run",
             "^TestApplySupersededLateObservationCannotQueueCommit$", "-count=1", "-v"],
            cwd=Path(__file__).resolve().parents[2] / "backend", text=True,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        )
        self.assertEqual(result.returncode, 0, result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
