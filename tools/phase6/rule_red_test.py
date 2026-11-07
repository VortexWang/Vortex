"""Supported reporter wrapping the actual PostgreSQL rule assertion."""
import subprocess
import unittest
from pathlib import Path

class RuleRevision(unittest.TestCase):
    def test_immutable_revision(self):
        result = subprocess.run(["go", "test", "./internal/service", "-run", "^TestRuleRevisionSchema$", "-count=1", "-v"], cwd=Path(__file__).resolve().parents[2] / "backend", text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode, 0, result.stdout)

if __name__ == "__main__":
    unittest.main(verbosity=2)
