"""Supported reporter wrapping the actual PostgreSQL ledger behavior test."""
import os
import subprocess
import unittest
from pathlib import Path

class LedgerAuthority(unittest.TestCase):
    def test_source_ledger_authority(self):
        result = subprocess.run(["go", "test", "./internal/service", "-run", os.getenv("LEDGER_RED_TARGET", "^TestLedgerSourceAuthoritySchema$"), "-count=1", "-v"], cwd=Path(__file__).resolve().parents[2] / "backend", text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode, 0, result.stdout)

if __name__ == "__main__":
    unittest.main(verbosity=2)
