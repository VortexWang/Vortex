"""Expose the real PostgreSQL Go assertion as a supported unittest RED report."""
import os
from pathlib import Path
import subprocess
import unittest

ROOT=Path(__file__).resolve().parents[2]
class ShareAuthority(unittest.TestCase):
    def test_share_lease_authority(self):
        result=subprocess.run(['go','test','./internal/service','-run','^TestShareLeaseAuthoritySchema$','-count=1','-v'],cwd=ROOT/'backend',env=os.environ,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode,0,result.stdout)
if __name__=='__main__':
    unittest.main(verbosity=2)
