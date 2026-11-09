"""Real PostgreSQL expiry must not depend on node availability or grants."""
import os
import subprocess
import unittest
from pathlib import Path

class PackageExpiry(unittest.TestCase):
    def test_authoritative_expiry(self):
        result = subprocess.run(['go', 'test', './internal/service', '-run', '^TestPackageLifecycleExpiryIndependentOfNodeAuthority$', '-count=1', '-v'], cwd=Path(__file__).resolve().parents[2]/'backend', env={**os.environ,'GOTOOLCHAIN':'local'}, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode, 0, result.stdout)

if __name__ == '__main__':
    unittest.main(verbosity=2)
