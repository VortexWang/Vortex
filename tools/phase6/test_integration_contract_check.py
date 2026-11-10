"""Mutation controls for the forward CR012 contract reconciliation gate."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SPEC=importlib.util.spec_from_file_location('cr012',Path(__file__).with_name('integration_contract_check.py'))
GATE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(GATE)

class IntegrationGate(unittest.TestCase):
 def test_candidate_and_adversarial_mutations(self):
  original=GATE.ROOT
  with contextlib.redirect_stdout(io.StringIO()):GATE.check()
  proposal=json.loads((original/GATE.PROPOSAL).read_text())
  paths={r['path'] for r in proposal['artifacts']+proposal['historical_records']+proposal['review_files']}|{GATE.PROPOSAL,'docs/governance/contract-review.yaml','.planning/REQUIREMENTS.md','tools/phase1/contracts.py'}
  if (original/GATE.APPROVAL).exists():paths.add(GATE.APPROVAL)
  with tempfile.TemporaryDirectory(prefix='vortex-cr012-negative-') as directory:
   clone=Path(directory)/'source'
   subprocess.run(['git','clone','--quiet','--shared',str(original),str(clone)],check=True)
   for p in paths:
    target=clone/p;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(original/p,target)
   for variant in ['missing','missing_history','duplicate','baseline','protected','wire','history','generated','review']:
    with self.subTest(variant=variant):
     changed=set()
     p=json.loads((original/GATE.PROPOSAL).read_text())
     if variant=='missing':p['artifacts'].pop()
     elif variant=='missing_history':p['historical_records'].pop()
     elif variant=='duplicate':p['artifacts'].append(p['artifacts'][0])
     elif variant=='baseline':p['artifacts'][0]['baseline_sha256']['phase6']='0'*64
     else:
      path={'protected':'contracts/database/schema.sql','wire':'contracts/protobuf/v1/vortex.proto','history':p['historical_records'][0]['path'],'generated':'contracts/protobuf/v1/vortex.pb.go','review':'docs/governance/contract-review.yaml'}[variant]
      raw=(clone/path).read_bytes()
      if variant=='wire':raw=raw.replace(b'credit_demand = 100;',b'credit_demand = 101;')
      elif variant=='review':
       value=json.loads(raw);value['change_approvals'][0]['decision']='rejected';raw=json.dumps(value).encode()
      else:raw+=b'\n-- invalid forward mutation\n'
      (clone/path).write_bytes(raw);changed.add(path)
      # Updating the candidate's hash cannot authorize normative or wire drift.
      if variant in ['protected','wire']:
       next(r for r in p['artifacts'] if r['path']==path)['sha256']=GATE.sha(raw)
     (clone/GATE.PROPOSAL).write_text(json.dumps(p));changed.add(GATE.PROPOSAL)
     with patch.object(GATE,'ROOT',clone),contextlib.redirect_stdout(io.StringIO()):
      with self.assertRaises(AssertionError):GATE.check()
     for path in changed:shutil.copyfile(original/path,clone/path)

if __name__=='__main__':unittest.main()
