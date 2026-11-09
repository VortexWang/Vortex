"""Supported reporter around real PostgreSQL port authority assertions."""
import subprocess
import unittest
from pathlib import Path

class PortAuthority(unittest.TestCase):
    def test_port_authority(self):
        result = subprocess.run(["go", "test", "./internal/service", "-run", "^TestPortReservationProofSchema$", "-count=1", "-v"], cwd=Path(__file__).resolve().parents[2] / "backend", text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode, 0, result.stdout)

if __name__ == "__main__":
    unittest.main(verbosity=2)
