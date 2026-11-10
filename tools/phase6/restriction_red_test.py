"""Run the actual PostgreSQL recovery assertion through supported unittest output."""
import subprocess
import unittest
from pathlib import Path


class RestrictionRecovery(unittest.TestCase):
    def test_all_causes_recovery(self):
        result = subprocess.run(
            ["go", "test", "./internal/service", "-run", "^TestRestrictionAllCauses$", "-count=1", "-v"],
            cwd=Path(__file__).resolve().parents[2] / "backend",
            text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        )
        self.assertEqual(result.returncode, 0, result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
