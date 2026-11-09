#!/usr/bin/env python3
"""Verify final Phase6 host and paired evidence from explicit raw artifacts."""
import datetime
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys

ROOT=Path(__file__).resolve().parents[2]
HOST_FILES=['commit.txt','host-source-identity.txt','host-source-status-before.txt','host-source-status-after.txt','compose.yml','docker-version.txt','docker-info.json','images.json','postgres-version.txt','valkey-version.txt','runner.log','cloudflare.log']
CONTAINER_FILES=['source-identity.txt','source-status-before.txt','source-status-after.txt','host.txt','sqlc-install.log','sqlc-diff.log','sqlc-output-count.txt','native-build-metadata.txt','race-build-metadata.txt','cross-build-metadata.txt','migrate.log','backend-normal.jsonl','backend-race.jsonl','completion.txt','reconciliation-normal.json','reconciliation-race.json','root-normal.jsonl','root-race.jsonl','engine-normal.jsonl','engine-race.jsonl','ledger-fault.log','share-fault.log','protobuf-install.log','protobuf-diff.log','exact-freeze.log','openapi.log','dependencies.log','historical-vlink-freeze.log','historical-vlink-freeze.exit','final-completion.txt']

def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def load(path):return json.loads(path.read_text())
def go_events(path):
 events=[json.loads(line) for line in path.read_text().splitlines() if line.strip()]
 assert events and not any(e.get('Action')=='fail' or (e.get('Action')=='skip' and 'Test' in e) for e in events), 'failed/skipped test: '+path.name
 assert not any('WARNING: DATA RACE' in e.get('Output','') for e in events), 'race detected'
 return events

def utc_nanoseconds(value):
 # Go's RFC3339Nano emits 0..9 fractional digits. Python 3.9's fromisoformat
 # accepts only selected microsecond widths; parse explicitly and retain all
 # nanoseconds so host Python versions enforce the same timing assertion.
 match=re.fullmatch(r'(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z',value)
 assert match, 'invalid UTC RFC3339Nano timestamp'
 base=datetime.datetime.strptime(match.group(1),'%Y-%m-%dT%H:%M:%S').replace(tzinfo=datetime.timezone.utc)
 return int(base.timestamp())*1000000000+int((match.group(2) or '').ljust(9,'0'))

def qualification(path,source):
 r=load(path)
 assert r['commit']==source and r['status']=='passed'
 assert (r['rules'],r['resolved'],r['members'],r['observed'])==(1000,1000,2000,2000)
 assert 0<=r['elapsed_ns']<=60000000000 and r['elapsed_ms']==r['elapsed_ns']/1000000
 assert re.fullmatch('[0-9a-f]{64}',r['inventory_sha256'])
 start,end=(utc_nanoseconds(r[k]) for k in ('started_at','ended_at'))
 assert 0<=end-start and abs(end-start-r['elapsed_ns'])<10000000
 assert 'deterministic node adapter' in r['scope']
 return r['elapsed_ms']

def runtime(folder,source):
 for name in HOST_FILES+CONTAINER_FILES:assert (folder/name).is_file(), 'missing '+name
 assert (folder/'commit.txt').read_text()==source+'\n'
 for name in ['host-source-identity.txt','source-identity.txt']:
  identity=(folder/name).read_text().splitlines();assert len(identity)==2 and identity[0]==source and re.fullmatch('[0-9a-f]{40}',identity[1])
 for name in ['host-source-status-before.txt','host-source-status-after.txt','source-status-before.txt','source-status-after.txt','sqlc-diff.log','protobuf-diff.log']:
  assert not (folder/name).read_text().strip(), 'source/generated drift: '+name
 assert int((folder/'sqlc-output-count.txt').read_text())==33
 spec=importlib.util.spec_from_file_location('rest_evidence_final',ROOT/'tools/phase6/rest_evidence_check.py');rest=importlib.util.module_from_spec(spec);spec.loader.exec_module(rest)
 base=rest.check(folder,source)
 tests={}
 for mode in ['normal','race']:
  back=go_events(folder/f'backend-{mode}.jsonl');passes={(e.get('Package'),e.get('Test')) for e in back if e.get('Action')=='pass'}
  assert ('vortex/backend/internal/service','TestReconcile1000Within60s') in passes
  for name in ['TestTracerDesiredStateReceipt','TestTracerRealProcesses']:
   assert ('vortex/backend/internal/service',name) in passes, 'real control-plane process proof missing'
  assert ('vortex/backend/internal/worker','TestRuleReconciliationDrainsReadyWorkAndCancels') in passes
  for name in ['TestRuleWorkerAuthorityWaitDoesNotHoldTaskLock','TestWorkerEventWaitDoesNotHoldBusinessLocks','TestExpiredControlEventFailureKeepsAttemptRecoverable']:
   assert ('vortex/backend/internal/service',name) in passes
  tests['reconciliation_'+mode+'_ms']=qualification(folder/f'reconciliation-{mode}.json',source)
  for family in ['root','engine']:
   events=go_events(folder/f'{family}-{mode}.jsonl');packages={e['Package'] for e in events if e.get('Action')=='pass' and 'Test' not in e}
   expected=['vortex/agent','vortex/agent/store','vortex/agent/upgrade','vortex/hub','vortex/contracts'] if family=='root' else ['vortex/engine/relay','vortex/engine/vlink','vortex/engine/runtime/rule','vortex/engine/accounting']
   assert set(expected)<=packages, 'missing complete '+family+' package'
   test_names={e.get('Test') for e in events if e.get('Action')=='pass'}
   if family=='root':assert {'TestHandoffFullDiskDoesNotPartiallyTransfer','TestServiceFullDiskCannotStopOrSelectCandidate','TestRenewalSenderYieldsAndRotatesUnansweredJournal','TestAutomaticRenewalObservesCommittedRulesOutsideLegacyScope','TestHubControlObservationPinsSnapshotBeforeClock'}<=test_names, 'isolated disk or control fairness proof missing'
   tests[family+'_'+mode+'_passed']=sum(1 for e in events if e.get('Action')=='pass' and 'Test' in e)
 for name,required in {
  'ledger-fault.log':['TestUsageFaultPowerLossAuditTransactionRollback','TestUsageActualAgentHubPostgresLostAckRestartAndOriginalSequence','TestUnknownRiskBudget72HourFreezeLateExactAndAuditRollback'],
  'share-fault.log':['TestShareHandoffFaultSIGKILLAfterDurableCASAndExpiredWorkerRecovery','TestRuntimeLeaseActualMTLS60SecondsAgentInstallAndLostBackendReceipts','TestHubRecoveryAdmissionActualMTLSExplicitProofFULLLossReconnectAndNewIssue'],
 }.items():
  raw=(folder/name).read_text();assert '\nFAIL' not in raw and '--- SKIP:' not in raw and 'WARNING: DATA RACE' not in raw
  for test in required:assert re.search(r'--- PASS: '+test+r' \(',raw), 'missing fault '+test
 for name,marker in [('exact-freeze.log','owner approval=approved'),('openapi.log','45 business operations'),('dependencies.log','two clean generations and committed source match byte-for-byte'),('final-completion.txt','phase6 backend/root/engine normal/race')]:assert marker in (folder/name).read_text(), 'incomplete '+name
 return dict(backend=base['tests'],checks=tests)

def ownership():
 rows=re.findall(r'^\| ([A-Z][A-Z0-9-]+) \| (06-\d\d) \|',(ROOT/'docs/coverage/phase6/COVERAGE.md').read_text(),re.M)
 assert len(rows)==96 and len(dict(rows))==96
 planned={}
 for path in (ROOT/'.planning/phases/06-m4-m4-backend').glob('06-??-PLAN.md'):
  for req in re.search(r'^requirements: \[(.*?)\]',path.read_text(),re.M).group(1).split(', '):
   assert req not in planned, 'duplicate requirement owner'
   planned[req]=path.name[:5]
 assert dict(rows)==planned, '96-item ownership drift'

def stamp(folder,source,producer,code,cf):
 folder=Path(folder);assert producer in ['docker','actions']
 validation=None;error=None
 if code==0:
  try:validation=runtime(folder,source)
  except (AssertionError,KeyError,ValueError,OSError) as e:code=1;error=str(e)
 files={str(p.relative_to(folder)):sha(p) for p in folder.rglob('*') if p.is_file() and p.name!='result.json'}
 metadata=(folder/'native-build-metadata.txt').read_text() if (folder/'native-build-metadata.txt').exists() else ''
 arch=re.search(r'GOARCH=(amd64|arm64)',metadata)
 result=dict(schema_version=1,commit=source,producer=producer,through_plan='06-11',runtime_status='passed' if code==0 else 'failed',exit_code=code,validation=validation,error=error,external_cloudflare=dict(exit_code=cf,status='passed' if cf==0 else 'not_run' if cf==78 else 'failed'),acceptance_complete=code==0 and cf==0,checksums=files,host=platform.platform(),run_id=os.getenv('GITHUB_RUN_ID'),repository=os.getenv('GITHUB_REPOSITORY'),runner_arch=os.getenv('RUNNER_ARCH'),execution_arch=arch.group(1) if arch else None,runner_image=os.getenv('ImageVersion'),historical_vlink_scope='pre-existing Phase3 review mismatch is recorded separately; no approval or global freeze pass granted')
 (folder/'result.json').write_text(json.dumps(result,indent=2)+'\n')
 print(json.dumps({k:result[k] for k in ['commit','producer','runtime_status','external_cloudflare','acceptance_complete','error']}))

def host(folder,source):
 ownership()
 folder=Path(folder);r=load(folder/'result.json')
 assert re.fullmatch('[0-9a-f]{40}',source) and r['commit']==source and r['through_plan']=='06-11'
 assert r['producer'] in ['docker','actions'] and r['runtime_status']=='passed' and r['exit_code']==0
 assert r['execution_arch'] in ['amd64','arm64']
 if r['producer']=='actions':assert str(r['run_id']).isdigit() and r['repository']=='VortexWang/Vortex'
 actual_files={str(p.relative_to(folder)) for p in folder.rglob('*') if p.is_file() and p.name!='result.json'}
 assert actual_files==set(r['checksums']), 'unrecorded/missing evidence file'
 for name,digest in r['checksums'].items():
  p=folder/name;assert p.resolve().is_relative_to(folder.resolve()) and p.is_file() and sha(p)==digest, 'evidence drift: '+name
 assert set(HOST_FILES+CONTAINER_FILES)<=set(r['checksums']), 'missing explicit evidence hash'
 assert runtime(folder,source)==r['validation'], 'semantic validation drift'
 cf=r['external_cloudflare'];assert cf in [dict(exit_code=0,status='passed'),dict(exit_code=78,status='not_run')]
 if cf['status']=='passed':assert '--- PASS: TestCloudflareControlledCRUD' in (folder/'cloudflare.log').read_text()
 assert r['acceptance_complete']==(cf['status']=='passed')
 return r

def pair(evidence,source):
 evidence=Path(evidence)
 d=host(evidence/'docker',source);runs={}
 assert d['producer']=='docker' and d['acceptance_complete']
 for arch in ['amd64','arm64']:
  a=host(evidence/('actions-'+arch),source)
  assert a['producer']=='actions' and a['execution_arch']==arch
  assert a['acceptance_complete'], 'external Cloudflare acceptance missing on Actions '+arch
  subprocess.run(['cmp','--silent',str(evidence/'docker/commit.txt'),str(evidence/('actions-'+arch)/'commit.txt')],check=True)
  runs[arch]=a['run_id']
 return dict(commit=source,status='passed',scope='Phase6 bounded backend reconciliation and runtime/REST/fault same-SHA pair; historical VLink review debt and Phase4/7/8 qualification excluded',actions_run_ids=runs)

if __name__=='__main__':
 action=sys.argv[1]
 if action=='stamp':stamp(sys.argv[2],sys.argv[3],sys.argv[4],int(sys.argv[5]),int(sys.argv[6]))
 elif action=='host':
  result=host(sys.argv[2],sys.argv[3]);print('host runtime evidence passed; full acceptance='+str(result['acceptance_complete']))
 elif action=='pair':print(json.dumps(pair(sys.argv[2],sys.argv[3]),indent=2))
 else:raise SystemExit('unknown evidence operation')
