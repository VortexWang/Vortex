"""Supported reporter for the actual PostgreSQL downgrade lifecycle test."""
import subprocess
import unittest
from pathlib import Path


class DowngradeAuthority(unittest.TestCase):
    def test_future_confirmation(self):
        result = subprocess.run(
            ["go", "test", "./internal/service", "-run", "^TestDowngradePreviewConfirmDueAuthority$", "-count=1", "-v"],
            cwd=Path(__file__).resolve().parents[2] / "backend",
            text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        )
        self.assertEqual(result.returncode, 0, result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
