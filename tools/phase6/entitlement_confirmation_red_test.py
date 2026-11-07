"""Run real PostgreSQL assertions for cross-package code confirmation."""
import os
import subprocess
import unittest
from pathlib import Path

class EntitlementConfirmation(unittest.TestCase):
    def test_cross_package_confirmation(self):
        root = Path(__file__).resolve().parents[2]
        result = subprocess.run([os.environ.get('GO', 'go'), 'test', './internal/service', '-run', '^TestEntitlementDifferentPackageConfirmationAtomic$', '-count=1', '-v'], cwd=root/'backend', env={**os.environ, 'GOTOOLCHAIN':'local'}, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode, 0, result.stdout)

if __name__ == '__main__':
    unittest.main(verbosity=2)
