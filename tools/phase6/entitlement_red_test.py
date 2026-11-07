"""Supported reporter for the actual atomic redemption contract test."""
import subprocess
import unittest
from pathlib import Path

class EntitlementAuthority(unittest.TestCase):
    def test_atomic_redemption(self):
        result = subprocess.run(["go", "test", "./internal/service", "-run", "^TestEntitlementAtomicRedemptionContract$", "-count=1", "-v"], cwd=Path(__file__).resolve().parents[2] / "backend", text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode, 0, result.stdout)

if __name__ == "__main__":
    unittest.main(verbosity=2)
