#!/usr/bin/env python3
"""Exercise runtime/pair evidence gates without running or editing source tests."""
import contextlib
import copy
import importlib.util
import io
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

HERE=Path(__file__).parent
spec=importlib.util.spec_from_file_location('final_evidence',HERE/'final_evidence_check.py');gate=importlib.util.module_from_spec(spec);spec.loader.exec_module(gate)
spec=importlib.util.spec_from_file_location('rest_fixture',HERE/'test_rest_evidence_check.py');fixture=importlib.util.module_from_spec(spec);spec.loader.exec_module(fixture)

class FinalEvidence(unittest.TestCase):
 def test_go_rfc3339nano_precision_and_invalid_timestamps(self):
  base=gate.utc_nanoseconds('2026-11-09T00:00:00Z')
  for fraction in ['1','123','123456','1234567','12345678','123456789']:
   self.assertEqual(gate.utc_nanoseconds('2026-11-09T00:00:00.'+fraction+'Z')-base,int(fraction.ljust(9,'0')))
  self.assertEqual(gate.utc_nanoseconds('2026-11-09T00:00:01.000000001Z')-gate.utc_nanoseconds('2026-11-09T00:00:00.999999999Z'),2)
  for invalid in ['2026-11-09T00:00:00.1234567890Z','2026-11-09T00:00:00Zjunk','2026-11-09T00:00:00+00:00','2026-11-09T00:00:00.Z','2026-13-09T00:00:00Z']:
   with self.assertRaises((AssertionError,ValueError)):gate.utc_nanoseconds(invalid)

 def fixture(self,folder,producer='docker',arch='arm64'):
  folder.mkdir()
  source=fixture.EvidenceGate().fixture(folder)
  for name in gate.HOST_FILES+gate.CONTAINER_FILES:
   if not (folder/name).exists():(folder/name).write_text('')
  (folder/'commit.txt').write_text(source+'\n')
  (folder/'host-source-identity.txt').write_text(source+'\n'+'b'*40+'\n')
  (folder/'exact-freeze.log').write_text('owner approval=approved')
  (folder/'openapi.log').write_text('45 business operations')
  (folder/'dependencies.log').write_text('two clean generations and committed source match byte-for-byte')
  (folder/'final-completion.txt').write_text('phase6 backend/root/engine normal/race')
  (folder/'native-build-metadata.txt').write_text((folder/'native-build-metadata.txt').read_text()+'GOARCH='+arch+'\n')
  for mode in ['normal','race']:
   with (folder/f'backend-{mode}.jsonl').open('a') as out:
    for package,test in [('service','TestReconcile1000Within60s'),('service','TestTracerDesiredStateReceipt'),('service','TestTracerRealProcesses'),('service','TestRuleWorkerAuthorityWaitDoesNotHoldTaskLock'),('service','TestWorkerEventWaitDoesNotHoldBusinessLocks'),('service','TestExpiredControlEventFailureKeepsAttemptRecoverable'),('worker','TestRuleReconciliationDrainsReadyWorkAndCancels')]:out.write(json.dumps(dict(Action='pass',Package='vortex/backend/internal/'+package,Test=test))+'\n')
   r=dict(commit=source,status='passed',rules=1000,resolved=1000,members=2000,observed=2000,elapsed_ns=1000000,elapsed_ms=1,started_at='2026-11-09T00:00:00Z',ended_at='2026-11-09T00:00:00.001Z',inventory_sha256='a'*64,scope='real PG; deterministic node adapter')
   (folder/f'reconciliation-{mode}.json').write_text(json.dumps(r))
   for family,packages in [('root',['vortex/'+p for p in ['agent','agent/store','agent/upgrade','hub','contracts']]),('engine',['vortex/engine/'+p for p in ['relay','vlink','runtime/rule','accounting']])]:
    events=[dict(Action='pass',Package=p) for p in packages]
    if family=='root':events.extend(dict(Action='pass',Package='vortex/agent/store',Test=t) for t in ['TestHandoffFullDiskDoesNotPartiallyTransfer','TestServiceFullDiskCannotStopOrSelectCandidate','TestRenewalSenderYieldsAndRotatesUnansweredJournal','TestAutomaticRenewalObservesCommittedRulesOutsideLegacyScope','TestHubControlObservationPinsSnapshotBeforeClock'])
    (folder/f'{family}-{mode}.jsonl').write_text(''.join(json.dumps(e)+'\n' for e in events))
  for name,names in [('ledger-fault.log',['TestUsageFaultPowerLossAuditTransactionRollback','TestUsageActualAgentHubPostgresLostAckRestartAndOriginalSequence','TestUnknownRiskBudget72HourFreezeLateExactAndAuditRollback']),('share-fault.log',['TestShareHandoffFaultSIGKILLAfterDurableCASAndExpiredWorkerRecovery','TestRuntimeLeaseActualMTLS60SecondsAgentInstallAndLostBackendReceipts','TestHubRecoveryAdmissionActualMTLSExplicitProofFULLLossReconnectAndNewIssue'])]:
   (folder/name).write_text(''.join('--- PASS: '+name+' (1s)\n' for name in names))
  (folder/'cloudflare.log').write_text('--- PASS: TestCloudflareControlledCRUD (1s)\n')
  with patch.dict(gate.os.environ,{'GITHUB_RUN_ID':'12345','GITHUB_REPOSITORY':'VortexWang/Vortex'}),contextlib.redirect_stdout(io.StringIO()):gate.stamp(folder,source,producer,0,0)
  return source

 def test_runtime_and_tampering(self):
  with tempfile.TemporaryDirectory() as tmp:
   root=Path(tmp);base=root/'base';source=self.fixture(base)
   self.assertTrue(gate.host(base,source)['acceptance_complete'])
   for variant in ['checksum','skip','partial','slow','missing','dirty','source','no_cloudflare','missing_lock_proof']:
    with self.subTest(variant=variant):
     target=root/variant;shutil.copytree(base,target)
     if variant=='missing_lock_proof':
      p=target/'backend-normal.jsonl';p.write_text(''.join(line for line in p.read_text().splitlines(True) if 'TestWorkerEventWaitDoesNotHoldBusinessLocks' not in line))
     if variant=='checksum':(target/'openapi.log').write_text('tampered')
     if variant=='skip':
      with (target/'root-race.jsonl').open('a') as f:f.write(json.dumps(dict(Action='skip',Package='vortex/agent',Test='TestOops'))+'\n')
     if variant in ['partial','slow']:
      p=target/'reconciliation-normal.json';value=json.loads(p.read_text())
      if variant=='partial':value['resolved']=999
      else:value['elapsed_ns']=60000000001;value['elapsed_ms']=60000.000001
      p.write_text(json.dumps(value))
     if variant=='missing':(target/'share-fault.log').unlink()
     if variant=='dirty':(target/'source-status-after.txt').write_text(' M backend/main.go\n')
     if variant=='source':(target/'commit.txt').write_text('b'*40+'\n')
     if variant=='no_cloudflare':(target/'cloudflare.log').write_text('not executed')
     # Re-hash the mutation too: semantic guards must still reject it, not only
     # checksums. Retain stale checksum only for the checksum-specific negative.
     if variant!='checksum':
      result=json.loads((target/'result.json').read_text());result['checksums']={str(p.relative_to(target)):gate.sha(p) for p in target.rglob('*') if p.is_file() and p.name!='result.json'};(target/'result.json').write_text(json.dumps(result))
     with self.assertRaises((AssertionError,KeyError,OSError)):gate.host(target,source)

 def test_exact_pair_arch_and_missing_external(self):
  with tempfile.TemporaryDirectory() as tmp:
   root=Path(tmp);source=self.fixture(root/'docker')
   for arch in ['amd64','arm64']:self.fixture(root/('actions-'+arch),'actions',arch)
   self.assertEqual(gate.pair(root,source)['status'],'passed')
   for variant in ['arch','external','sha','run_id']:
    p=root/'actions-amd64/result.json';original=p.read_bytes();value=json.loads(original)
    if variant=='arch':value['execution_arch']='arm64'
    if variant=='external':value['external_cloudflare']=dict(exit_code=78,status='not_run');value['acceptance_complete']=False
    if variant=='sha':value['commit']='b'*40
    if variant=='run_id':value['run_id']=None
    p.write_text(json.dumps(value))
    with self.assertRaises(AssertionError):gate.pair(root,source)
    p.write_bytes(original)
if __name__=='__main__':unittest.main()
