"""Mutation controls against the complete approved contract verifier."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]

class ApprovalIntegrity(unittest.TestCase):
    def test_exact_approval_and_mutations(self):
        with tempfile.TemporaryDirectory(prefix='vortex-contract-negative-') as folder:
            clone = Path(folder)/'source'
            subprocess.run(['git','clone','--quiet','--no-hardlinks',str(ROOT),str(clone)],check=True)
            # Also support the pre-commit validation run without changing source.
            owned = ['tools/phase6/rule_contract_check.py','docs/governance/contract-review.yaml','docs/changes/CR-006-APPROVAL.json','docs/changes/CR-007-metering-ledger.json','docs/changes/CR-007-APPROVAL.json']+[r['path'] for r in json.loads((ROOT/'docs/changes/CR-007-metering-ledger.json').read_text())['artifacts']]
            for name in owned:
                shutil.copyfile(ROOT/name,clone/name)
            work = Path(folder)/'work'
            work.mkdir()
            def verify():
                return subprocess.run(['python3','tools/phase6/rule_contract_check.py',str(work)],cwd=clone,env={**os.environ,'PYTHONDONTWRITEBYTECODE':'1'},text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
            result=verify()
            self.assertEqual(result.returncode,0,result.stdout)
            for name,kind in [('docs/changes/CR-006-APPROVAL.json','owner'),('docs/governance/contract-review.yaml','history'),('contracts/database/schema.sql','normative'),('contracts/versions.yaml','catalog'),('contracts/CHANGELOG.md','changelog'),('docs/changes/CR-007-APPROVAL.json','owner7'),('docs/changes/CR-007-APPROVAL.json','proposal7'),('docs/changes/CR-007-APPROVAL.json','companion7'),('docs/changes/CR-007-APPROVAL.json','hash7'),('docs/changes/CR-007-APPROVAL.json','duplicate7'),('docs/changes/CR-007-APPROVAL.json','time7'),('docs/governance/contract-review.yaml','duplicate_review7'),('contracts/protobuf/v1/lifecycle.md','companion_bytes7'),('docs/changes/CR-007-metering-ledger.json','proposal_bytes7')]:
                with self.subTest(kind=kind):
                    path=clone/name
                    original=path.read_bytes()
                    if kind in ('owner','history','catalog','owner7','proposal7','companion7','hash7','duplicate7','time7','duplicate_review7','proposal_bytes7'):
                        value=json.loads(original)
                        if kind=='owner': value['owner_response']='continue'
                        elif kind=='history': value['change_approvals'][0]['decision']='rejected'
                        elif kind=='catalog': value['contracts'][0]['sources'].append('unapproved source')
                        elif kind=='owner7': value['owner_response']='继续'
                        elif kind=='proposal7': value['proposal_sha256']='0'*64
                        elif kind=='companion7': value['changes'][2]['artifacts'].pop()
                        elif kind=='hash7': value['changes'][0]['contract_sha256']='0'*64
                        elif kind=='duplicate7': value['changes'].append(value['changes'][-1])
                        elif kind=='time7': value['recorded_at']='2026-01-01T00:00:00Z'
                        elif kind=='proposal_bytes7': value['acceptance']['owner_approval']=True
                        else: value['change_approvals'].append(value['change_approvals'][-1])
                        path.write_text(json.dumps(value))
                    else:
                        path.write_bytes(original+b'\nunapproved mutation\n')
                    result=verify()
                    self.assertNotEqual(result.returncode,0,'mutation accepted: '+kind)
                    self.assertIn('AssertionError',result.stdout,result.stdout)
                    path.write_bytes(original)

if __name__=='__main__':
    unittest.main(verbosity=2)
