#!/usr/bin/env python3
"""Reject tampered candidate inventories in isolated filesystem copies."""
import contextlib
import copy
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SPEC=importlib.util.spec_from_file_location('rest_contract',Path(__file__).with_name('rest_contract_check.py'))
GATE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(GATE)

class CandidateGate(unittest.TestCase):
 def test_original_and_tampered_candidates(self):
  original=GATE.ROOT
  proposal=json.loads((original/'docs/changes/CR-009-rest-events.json').read_text())
  expected=GATE.expected_paths()
  doc_names=subprocess.check_output(['git','ls-tree','-r','-z','--name-only',GATE.BASE,'--','docs/changes'],cwd=original)
  paths=expected|{'docs/governance/contract-review.yaml','docs/changes/CR-009-rest-events.json','docs/changes/CR-009-APPROVAL.json'}|set(filter(None,doc_names.decode().split('\0')))
  historical={p:GATE.historical(p) for p in paths}
  # Baseline metadata is read from immutable git objects, while candidate files
  # are read from the isolated copy. No working-tree mutation or restore.
  with contextlib.redirect_stdout(io.StringIO()):GATE.check()
  real_output=subprocess.check_output
  def git_output(*args,**kwargs):
   kwargs['cwd']=original
   return real_output(*args,**kwargs)
  for variant in ['missing','duplicate','hash','protected','history','semver','owner','approval_hash','approval_duplicate','approval_time','governance_duplicate','catalog','notice']:
   with self.subTest(variant=variant), tempfile.TemporaryDirectory() as directory:
    root=Path(directory)
    for p in paths:
     target=root/p;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes((original/p).read_bytes())
    candidate=copy.deepcopy(proposal)
    if variant=='missing':candidate['artifacts'].pop()
    if variant=='duplicate':candidate['artifacts'].append(candidate['artifacts'][0])
    if variant=='hash':candidate['artifacts'][0]['sha256']='0'*64
    if variant=='protected':
     p='backend/db/migrations/00008_limits_shares.sql';(root/p).write_bytes((root/p).read_bytes()+b'\n-- unexpected mutation\n')
     next(r for r in candidate['artifacts'] if r['path']==p)['sha256']=GATE.digest((root/p).read_bytes())
    if variant=='history':
     p='docs/changes/CR-008-APPROVAL.json';(root/p).write_bytes((root/p).read_bytes()+b'\n')
    if variant=='semver':candidate['changes'][1]['kind']='compatible'
    if variant in ['owner','approval_hash','approval_duplicate','approval_time']:
     p='docs/changes/CR-009-APPROVAL.json';value=json.loads((root/p).read_text())
     if variant=='owner':value['owner_response']='approved automatically'
     if variant=='approval_hash':value['proposal_sha256']='0'*64
     if variant=='approval_duplicate':value['changes'].append(value['changes'][0])
     if variant=='approval_time':value['changes'][0]['approved_at']='2000-01-01T00:00:00Z'
     (root/p).write_text(json.dumps(value))
    if variant=='governance_duplicate':
     p='docs/governance/contract-review.yaml';value=json.loads((root/p).read_text());value['change_approvals'].append(value['change_approvals'][-1]);(root/p).write_text(json.dumps(value))
    if variant=='catalog':
     p='contracts/versions.yaml';value=json.loads((root/p).read_text());value['contracts'][0]['sources'].append('not approved');(root/p).write_text(json.dumps(value))
    if variant=='notice':
     p='contracts/CHANGELOG.md';(root/p).write_bytes((root/p).read_bytes()+b'\nextra text\n')
    (root/'docs/changes/CR-009-rest-events.json').write_text(json.dumps(candidate))
    with patch.object(GATE,'ROOT',root),patch.object(GATE,'historical',lambda p:historical.get(p)),patch.object(GATE,'expected_paths',lambda:expected),patch.object(GATE.subprocess,'check_output',side_effect=git_output):
     with self.assertRaises(AssertionError):GATE.check()
if __name__=='__main__':unittest.main()
