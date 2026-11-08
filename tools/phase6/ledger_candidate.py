"""Generate the reviewable CR-007 candidate; never create an approval."""
import hashlib
import json
from pathlib import Path
import re
import subprocess

ROOT=Path(__file__).resolve().parents[2]
BASE='9749c7af71b5b6d430f58b67ef5398c0adc90ce2'
def dump(p,value):
    (ROOT/p).write_text(json.dumps(value,indent=2,ensure_ascii=False)+'\n')
def load(p): return json.loads((ROOT/p).read_text())
def historical(p): return subprocess.check_output(['git','show',BASE+':'+p],cwd=ROOT,stderr=subprocess.DEVNULL)
def digest(raw): return hashlib.sha256(raw).hexdigest()

catalog=load('contracts/versions.yaml')
changes=[]
for contract in catalog['contracts']:
    if contract['id'] in ('vortex.database','vortex.public-api','vortex.control.v1'):
        previous='1.4.0' if contract['id']=='vortex.control.v1' else '2.1.0'
        version='1.5.0' if contract['id']=='vortex.control.v1' else '2.2.0'
        contract['version'],contract['status']=version,'candidate'
        changes.append(dict(contract_id=contract['id'],**{'from':previous},version=version,kind='compatible'))
dump('contracts/versions.yaml',catalog)

schema=load('contracts/protobuf/v1/lifecycle.schema.json')
capabilities=schema['$defs']['hello']['properties']['capabilities']['items']['enum']
if 'usage-ledger-v1' not in capabilities: capabilities.append('usage-ledger-v1')
schema['$defs']['usage_envelope']={
 'type':'object','additionalProperties':False,
 'required':['version','node_id','origin_boot_id','report_boot_id','sequence','batch_id','kind','payload','payload_sha256'],
 'properties':{'version':{'const':1},**{key:{'type':'string','format':'uuid'} for key in ('node_id','origin_boot_id','report_boot_id','batch_id')},
 'sequence':{'type':'integer','minimum':1,'maximum':9223372036854775807},'kind':{'enum':['known','unknown']},
 'payload':{'type':'string','contentEncoding':'base64','maxLength':1398104},'payload_sha256':{'type':'string','contentEncoding':'base64','minLength':44,'maxLength':44}}}
dump('contracts/protobuf/v1/lifecycle.schema.json',schema)

mapping=load('contracts/requirements-map.yaml')
for row in mapping['requirements']:
    if row['id'].startswith('MTR-'):
        row.update(implementation_plan='06-05',contract_candidate='CR-007',verification=['docs/coverage/phase6/LEDGER.md','tools/phase6/ledger_fault_injection.sh'],http_adapter_plan='06-07')
dump('contracts/requirements-map.yaml',mapping)

changelog=(ROOT/'contracts/CHANGELOG.md').read_text()
match=re.search(r'```json\s*(\{.*?\})\s*```',changelog,re.S)
record=json.loads(match.group(1))
for change in changes:
    record['changes']=[r for r in record['changes'] if r['contract_id']!=change['contract_id']]
    record['changes'].append({**change,'status':'pending-owner-approval','summary':'CR-007 immutable source ledger, idempotent metering and provenance-preserving Hub usage; historical approvals remain immutable.'})
changelog=changelog[:match.start(1)]+json.dumps(record,separators=(',',':'))+changelog[match.end(1):]
notice='''\n## Phase 6 source ledger and usage (06-05 candidate)\n\nCR-007 proposes database/API 2.2.0 and control 1.5.0 as compatible extensions.\nOnly a newly negotiated usage-ledger-v1 Hub LedgerRecord carries the versioned\noriginal node/report-boot/sequence/digest envelope. Protobuf tags and legacy\nwire encodings are unchanged. Existing sequence-less SQLite payloads are retained\nwithout invented provenance. Full signer and funded runtime producers remain\nplan 06-06; HTTP adapters remain 06-07; final paired acceptance remains 06-11.\nUnknown upper bounds remain frozen, distinct from measured bytes; 72 hours\nis never an automatic release. Compensation requires approved actual platform\nbacking and source-local atomic accounting. No owner approval is implied.\n'''
if '## Phase 6 source ledger and usage (06-05 candidate)' not in changelog: changelog+=notice
(ROOT/'contracts/CHANGELOG.md').write_text(changelog)

previous=load('docs/changes/CR-006-packages-entitlements.json')
paths={row['path'] for row in previous['artifacts']}|{'backend/db/migrations/00007_metering_ledger.sql'}
artifacts=[]
for path in sorted(paths):
    try: before=digest(historical(path))
    except subprocess.CalledProcessError: before=None
    artifacts.append(dict(path=path,sha256=digest((ROOT/path).read_bytes()),baseline_sha256=before))
proposal=dict(schema_version=1,id='CR-007',status='pending-owner-approval',created='2026-11-08',plan='06-05',baseline_source=BASE,
 prior_approval=dict(path='docs/changes/CR-006-APPROVAL.json',sha256=digest((ROOT/'docs/changes/CR-006-APPROVAL.json').read_bytes()),proposal_sha256='60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d'),
 changes=changes,candidate_versions={c['id']:c['version'] for c in catalog['contracts']},artifacts=artifacts,
 acceptance=dict(owner_approval=False,http_adapter_plan='06-07',signer_authority_plan='06-06',paired_acceptance_plan='06-11'))
dump('docs/changes/CR-007-metering-ledger.json',proposal)
print('CR-007 candidate SHA-256:',digest((ROOT/'docs/changes/CR-007-metering-ledger.json').read_bytes()))
