"""Supported RED reporter wrapping a real PostgreSQL behavioral assertion."""
import os
from pathlib import Path
import subprocess
import unittest
ROOT=Path(__file__).resolve().parents[2]
class ShareHandoff(unittest.TestCase):
    def test_handoff_receipt_authority(self):
        result=subprocess.run(['go','test','./internal/service','-run','^TestShareHandoffDurableReceiptAuthoritySchema$','-count=1','-v'],cwd=ROOT/'backend',env=os.environ,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        self.assertEqual(result.returncode,0,result.stdout)
if __name__=='__main__':
    unittest.main(verbosity=2)
