"""Supported reporter around the actual serving assertion in real PostgreSQL."""
import subprocess
import unittest
from pathlib import Path

class ServingBoundary(unittest.TestCase):
    def test_serving_boundary(self):
        result = subprocess.run(["go", "test", "./internal/service", "-run", "^TestServingRejectsDeletedMember$", "-count=1", "-v"], cwd=Path(__file__).resolve().parents[2] / "backend", text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode, 0, result.stdout)

if __name__ == "__main__":
    unittest.main(verbosity=2)
