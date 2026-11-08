"""Verify exact historical approvals, unchanged wire and the exact CR-007 approval."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
def load(path):
    return json.loads((ROOT / path).read_text())
def git_bytes(path):
    return subprocess.check_output(['git', 'show', proposal['baseline_source'] + ':' + path], cwd=ROOT)
def approved_bytes(path):
    return subprocess.check_output(['git', 'show', approved_source + ':' + path], cwd=ROOT)
def protoc(source, kind, text=None, data=None):
    args = ['protoc', '-I', str(source.parent), '--'+('encode' if text is not None else 'decode')+'=vortex.control.v1.'+kind, source.name]
    return subprocess.check_output(args, input=text.encode() if text is not None else data)

proposal = load('docs/changes/CR-005-rule-two-phase.json')
approval = load('docs/changes/CR-005-APPROVAL.json')
approved_source = 'fc63d57406f9f0e3eebe99ad276f95b004add710'
history6_source='9749c7af71b5b6d430f58b67ef5398c0adc90ce2'
def history6(path):
    return subprocess.check_output(['git','show',history6_source+':'+path],cwd=ROOT)
for path in ('docs/changes/CR-006-packages-entitlements.json','docs/changes/CR-006-APPROVAL.json'):
    assert (ROOT/path).read_bytes()==history6(path), 'historical CR-006 approval rewritten: '+path
for path in ('docs/changes/CR-005-rule-two-phase.json', 'docs/changes/CR-005-APPROVAL.json'):
    assert (ROOT/path).read_bytes() == approved_bytes(path), 'historical approval rewritten: '+path
proposal_hash = hashlib.sha256((ROOT / 'docs/changes/CR-005-rule-two-phase.json').read_bytes()).hexdigest()
assert approval['status'] == 'approved' and approval['owner_response'] == '批准 CR-005'
assert approval['proposal_sha256'] == proposal_hash == 'b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4', 'approved proposal changed'
catalog = load('contracts/versions.yaml')
changes = {row['contract_id']: row for row in proposal['changes']}
for row in proposal['artifacts']:
    raw = approved_bytes(row['path'])
    if row['path'] == 'contracts/versions.yaml':
        assert hashlib.sha256(raw).hexdigest() == approval['frozen_catalog_sha256'], 'frozen catalog changed'
        original_catalog = json.loads(raw)
        for entry in original_catalog['contracts']:
            if entry['id'] in changes:
                assert entry['status'] == 'frozen', 'historical approved contract is not frozen'
                entry['status'] = 'candidate'
        raw = (json.dumps(original_catalog, indent=2)+'\n').encode()
        assert row['sha256'] == approval['candidate_catalog_sha256']
    assert hashlib.sha256(raw).hexdigest() == row['sha256'], 'historical approved candidate hash changed: '+row['path']
schema = load('contracts/runtime/config.schema.json')
assert schema['$defs']['legacy'] == json.loads(git_bytes('contracts/runtime/config.schema.json')), 'legacy schema changed'
review = json.loads(history6('docs/governance/contract-review.yaml'))
current_review = load('docs/governance/contract-review.yaml')
historical = json.loads(git_bytes('docs/governance/contract-review.yaml'))
old_approvals = historical['change_approvals']
assert review['change_approvals'][:len(old_approvals)] == old_approvals, 'historical approvals rewritten'
approved_review = json.loads(approved_bytes('docs/governance/contract-review.yaml'))
assert approved_review['change_approvals'][len(old_approvals):] == approval['changes'], 'CR-005 signed approval history mismatch'
assert review['change_approvals'][:len(approved_review['change_approvals'])] == approved_review['change_approvals'], 'CR-005 historical prefix rewritten'
assert len(approval['changes']) == len(changes), 'missing or duplicate approval'
for row in approval['changes']:
    change = changes[row['contract_id']]
    assert row['decision'] == 'approved' and row['proposal_sha256'] == proposal_hash
    assert row['from'] == change['from'] and row['version'] == change['version']
    assert row['reviewer'] == approval['reviewer'] and row['approved_at'] == approval['recorded_at']
    if row['contract_id']=='vortex.runtime-config':
        subprocess.run(['python3', 'tools/phase1/contracts.py', 'check-frozen', '--contract-id', row['contract_id']], cwd=ROOT, check=True)

candidate = load('docs/changes/CR-006-packages-entitlements.json')
candidate_hash = hashlib.sha256((ROOT/'docs/changes/CR-006-packages-entitlements.json').read_bytes()).hexdigest()
assert candidate_hash == '60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d', 'approved CR-006 proposal changed'
approved6 = load('docs/changes/CR-006-APPROVAL.json')
assert approved6['id']=='CR-006' and approved6['status']=='approved' and approved6['owner_response']=='批准 CR-006'
assert approved6['proposal_sha256']==candidate_hash and approved6['proposal_path']=='docs/changes/CR-006-packages-entitlements.json'
assert approved6['candidate_source']=='345dea19d0b5fd5d1fb0ca525f6426533b1cddba'
assert review['change_approvals'][len(approved_review['change_approvals']):]==approved6['changes'], 'exact CR-006 append mismatch'
assert len(approved6['changes'])==2 and {r['contract_id'] for r in approved6['changes']}=={'vortex.database','vortex.public-api'}
expected_review=json.loads(json.dumps(approved_review))
for row in approved6['changes']:
    assert row['from']=='2.0.0' and row['version']=='2.1.0' and row['decision']=='approved'
    assert row['proposal_sha256']==candidate_hash and row['reviewer']==approved6['reviewer']=='user-approved-in-session'
    assert row['approved_at']==approved6['recorded_at'] and row['approval_record']=='docs/changes/CR-006-APPROVAL.json'
    contract=next(c for c in expected_review['contracts'] if c['id']==row['contract_id'])
    contract['version'],contract['sha256']='2.1.0',row['contract_sha256']
    assert hashlib.sha256(history6(contract['path'])).hexdigest()==row['contract_sha256']
expected_review['change_approvals']+=approved6['changes']
assert review==expected_review, 'unapproved governance metadata mutation'
assert candidate['id']=='CR-006' and candidate['plan']=='06-04' and candidate['status']=='pending-owner-approval'
assert candidate['baseline_source']==approved_source
assert candidate['prior_approval']['sha256']==hashlib.sha256(approved_bytes('docs/changes/CR-005-APPROVAL.json')).hexdigest()
assert candidate['prior_approval']['proposal_sha256']==proposal_hash
assert candidate['changes']==[{'contract_id':c,'from':'2.0.0','version':'2.1.0','kind':'compatible'} for c in ('vortex.database','vortex.public-api')]
assert {x['id']:x['version'] for x in json.loads(history6('contracts/versions.yaml'))['contracts']} == candidate['candidate_versions'], 'historical candidate catalog version mismatch'
assert len(candidate['artifacts'])==len({x['path'] for x in candidate['artifacts']}), 'duplicate candidate artifact'
required={x['path'] for x in proposal['artifacts']}|{'backend/db/migrations/00006_packages_entitlements.sql','contracts/CHANGELOG.md'}
assert {x['path'] for x in candidate['artifacts']}==required, 'candidate artifact inventory incomplete'
for row in candidate['artifacts']:
    raw=history6(row['path'])
    candidate_raw=subprocess.check_output(['git','show',approved6['candidate_source']+':'+row['path']],cwd=ROOT)
    assert hashlib.sha256(candidate_raw).hexdigest()==row['sha256'], 'signed candidate source changed: '+row['path']
    if row['path']=='contracts/versions.yaml':
        assert hashlib.sha256(raw).hexdigest()==approved6['frozen_catalog_sha256'], 'frozen catalog hash mismatch'
        assert row['sha256']==approved6['candidate_catalog_sha256'], 'candidate catalog hash mismatch'
        thawed=json.loads(raw)
        for contract in thawed['contracts']:
            if contract['id'] in ('vortex.database','vortex.public-api'):
                assert contract['version']=='2.1.0' and contract['status']=='frozen'
                contract['status']='candidate'
        assert (json.dumps(thawed,indent=2)+'\n').encode()==candidate_raw, 'catalog changes exceed approved freeze metadata'
    elif row['path']=='contracts/CHANGELOG.md':
        expected_notice = '\n## Phase 6 packages and entitlements (06-04 approved)\n\nThe owner explicitly approved CR-006 with “批准 CR-006”, including consumption\non successful confirmation and no automatic refund or use restoration on later\napplication failure. The immutable proposal SHA-256 remains\n`60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d`.\nApproval is recorded in `docs/changes/CR-006-APPROVAL.json` and two appended\n`docs/governance/contract-review.yaml` records. Database/API 2.1.0 are frozen;\nonly approval metadata and this appended notice differ from candidate bytes.\nThe earlier candidate section remains an immutable historical statement.\n'
        assert raw==candidate_raw+expected_notice.encode(), 'changelog changes exceed exact appended approval notice'
    else:
        assert raw==candidate_raw, 'approved normative artifact changed: '+row['path']
    if row['baseline_sha256'] is not None:
        assert hashlib.sha256(approved_bytes(row['path'])).hexdigest()==row['baseline_sha256'], 'candidate baseline changed'
mutable={'contracts/versions.yaml','contracts/database/schema.sql','contracts/openapi/v1/openapi.yaml','backend/openapi/v1/openapi.yaml','contracts/requirements-map.yaml','contracts/protobuf/v1/lifecycle.schema.json','contracts/protobuf/v1/lifecycle.md'}
for row in proposal['artifacts']:
    if row['path'] not in mutable:
        assert (ROOT/row['path']).read_bytes()==approved_bytes(row['path']), 'unchanged approved wire/migration modified: '+row['path']
old_map=json.loads(approved_bytes('contracts/requirements-map.yaml'))
current_map=load('contracts/requirements-map.yaml')
assert len(old_map['requirements'])==len(current_map['requirements']), 'requirement inventory changed'
for old,current in zip(old_map['requirements'],current_map['requirements']):
    if old['id'].startswith('MTR-'):
        assert current['implementation_plan']=='06-05' and current['contract_candidate']=='CR-007' and current['verification']
        current={k:v for k,v in current.items() if k not in ('implementation_plan','contract_candidate','verification','http_adapter_plan')}
    if old['id'].startswith('PLN-'):
        assert current['implementation_plan']=='06-04' and current['contract_candidate']=='CR-006' and current['verification']
        current={k:v for k,v in current.items() if k not in ('implementation_plan','contract_candidate','verification','http_adapter_plan')}
    assert old==current, 'historical requirement mapping changed: '+old['id']
expected_schema='\n'.join([f'-- Phase 6 implemented PostgreSQL schema through migration {sorted((ROOT/"backend/db/migrations").glob("*.sql"))[-1].stem}.\n-- Canonical executable source: backend/db/migrations/*.sql.\n']+['\n'.join(line for line in p.read_text().split('-- +goose Down',1)[0].splitlines() if not line.startswith('-- +goose'))+'\n' for p in sorted((ROOT/'backend/db/migrations').glob('*.sql'))])
assert (ROOT/'contracts/database/schema.sql').read_text()==expected_schema, 'database review schema differs from executable migrations'
assert (ROOT / 'contracts/openapi/v1/openapi.yaml').read_bytes() == (ROOT / 'backend/openapi/v1/openapi.yaml').read_bytes(), 'OpenAPI copy differs'
for path in sorted((ROOT / 'backend/db/migrations').glob('0000[1-6]*.sql')):
    assert path.read_bytes() == history6(str(path.relative_to(ROOT))), 'applied migration rewritten: '+path.name
proposal7=load('docs/changes/CR-007-metering-ledger.json')
assert proposal7['id']=='CR-007' and proposal7['plan']=='06-05' and proposal7['status']=='pending-owner-approval'
assert proposal7['baseline_source']==history6_source
assert proposal7['prior_approval']['sha256']==hashlib.sha256(history6('docs/changes/CR-006-APPROVAL.json')).hexdigest()
assert proposal7['prior_approval']['proposal_sha256']==candidate_hash
expected_changes=[{'contract_id':key,'from':before,'version':after,'kind':'compatible'} for key,before,after in [('vortex.database','2.1.0','2.2.0'),('vortex.public-api','2.1.0','2.2.0'),('vortex.control.v1','1.4.0','1.5.0')]]
assert proposal7['changes']==expected_changes
assert proposal7['candidate_versions']=={c['id']:c['version'] for c in catalog['contracts']}
for entry in catalog['contracts']:
    if entry['id'] in {r['contract_id'] for r in expected_changes}: assert entry['status']=='frozen'
    else: assert entry==next(c for c in json.loads(history6('contracts/versions.yaml'))['contracts'] if c['id']==entry['id'])
required7={r['path'] for r in candidate['artifacts']}|{'backend/db/migrations/00007_metering_ledger.sql'}
assert len(proposal7['artifacts'])==len(required7) and {r['path'] for r in proposal7['artifacts']}==required7
proposal7_hash=hashlib.sha256((ROOT/'docs/changes/CR-007-metering-ledger.json').read_bytes()).hexdigest()
assert proposal7_hash=='3ddd1ab79c656b3684e64a3854f32785ff2fb8998743ac73353cdc90b086b0c5', 'approved CR-007 proposal changed'
approved7=load('docs/changes/CR-007-APPROVAL.json')
source7='995d7fca563c87cc8c3b38f3252e50b8ab94458e'
def candidate7(path):
    return subprocess.check_output(['git','show',source7+':'+path],cwd=ROOT)
assert (ROOT/'docs/changes/CR-007-metering-ledger.json').read_bytes()==candidate7('docs/changes/CR-007-metering-ledger.json')
expected7=dict(schema_version=1,id='CR-007',status='approved',proposal_path='docs/changes/CR-007-metering-ledger.json',proposal_sha256=proposal7_hash,owner_response='批准 CR-007',reviewer='user-approved-in-session',recorded_at='2026-11-08T02:49:25Z',candidate_source=source7,candidate_catalog_sha256=hashlib.sha256(candidate7('contracts/versions.yaml')).hexdigest(),frozen_catalog_sha256=hashlib.sha256((ROOT/'contracts/versions.yaml').read_bytes()).hexdigest(),changes=[])
expected_review7=json.loads(json.dumps(review))
for change in expected_changes:
    contract=next(c for c in expected_review7['contracts'] if c['id']==change['contract_id'])
    row={key:change[key] for key in ('contract_id','from','version')}
    row.update(proposal_sha256=proposal7_hash,contract_sha256=hashlib.sha256(candidate7(contract['path'])).hexdigest(),reviewer=expected7['reviewer'],approved_at=expected7['recorded_at'],decision='approved',approval_record='docs/changes/CR-007-APPROVAL.json')
    contract.update(version=change['version'],sha256=row['contract_sha256'])
    if 'artifacts' in contract:
        for artifact in contract['artifacts']:
            artifact['sha256']=hashlib.sha256(candidate7(artifact['path'])).hexdigest()
        row['artifacts']=json.loads(json.dumps(contract['artifacts']))
    expected7['changes'].append(row)
expected_review7['change_approvals']+=expected7['changes']
assert approved7==expected7, 'exact CR-007 owner/proposal/contract/companion approval mismatch'
assert current_review==expected_review7, 'exact CR-007 governance append mismatch'
for artifact in proposal7['artifacts']:
    candidate_raw=candidate7(artifact['path'])
    assert hashlib.sha256(candidate_raw).hexdigest()==artifact['sha256'], 'signed CR-007 source changed: '+artifact['path']
    raw=(ROOT/artifact['path']).read_bytes()
    if artifact['path']=='contracts/versions.yaml':
        thawed=json.loads(raw)
        for contract in thawed['contracts']:
            if contract['id'] in {r['contract_id'] for r in expected_changes}:
                assert contract['status']=='frozen'
                contract['status']='candidate'
        assert (json.dumps(thawed,indent=2,ensure_ascii=False)+'\n').encode()==candidate_raw, 'CR-007 catalog exceeds exact freeze metadata'
    elif artifact['path']=='contracts/CHANGELOG.md':
        assert raw==candidate_raw+'\n## Phase 6 source ledger and usage (06-05 approved)\n\nThe owner explicitly approved CR-007 with “批准 CR-007”. The immutable proposal\nSHA-256 remains `3ddd1ab79c656b3684e64a3854f32785ff2fb8998743ac73353cdc90b086b0c5`.\nApproval is recorded in `docs/changes/CR-007-APPROVAL.json` and three appended\n`docs/governance/contract-review.yaml` records. Database/API 2.2.0 and control\n1.5.0 are frozen, including the exact control lifecycle companions. Only approval\nmetadata and this appended notice differ from candidate bytes. The earlier\ncandidate section remains an immutable historical statement. CR-005/006 approval\nhistory is unchanged. Unknown liabilities require funded proof to close;\n06-06 signer/lease production, 06-07 HTTP and 06-11 paired acceptance remain due.\n'.encode(), 'CR-007 changelog exceeds exact approval notice'
    else:
        assert raw==candidate_raw, 'approved CR-007 normative bytes changed: '+artifact['path']
    if artifact['baseline_sha256'] is not None:
        assert hashlib.sha256(history6(artifact['path'])).hexdigest()==artifact['baseline_sha256']
assert proposal7['acceptance']['owner_approval'] is False, 'candidate history rewritten'
for row in expected7['changes']:
    subprocess.run(['python3','tools/phase1/contracts.py','check-frozen','--contract-id',row['contract_id']],cwd=ROOT,check=True)
subprocess.run(['python3', 'tools/phase1/contracts.py', 'check-change', '--base', history6_source], cwd=ROOT, check=True)
work=Path(sys.argv[1]); old=work/'baseline.proto'
old.write_bytes(git_bytes('contracts/protobuf/v1/vortex.proto'))
current=ROOT/'contracts/protobuf/v1/vortex.proto'
for kind,text in [
    ('NodeMessage','node_id:"node" boot_id:"boot" sequence:1 heartbeat{applied_config_version:7 observed_at:"2026-11-07T00:00:00Z"}'),
    ('ControlMessage','message_id:"message" sequence:2 config_update{version:7 payload:"{}" transaction_id:"transaction-0001" fencing_generation:3 payload_sha256:"digest" expected_version:6}'),
    ('ControlMessage','sequence:3 authorization_update{generation:5 payload:"{}"}'),
]:
    baseline=protoc(old,kind,text=text)
    assert baseline == protoc(current,kind,text=text), 'legacy field/tag encoding changed'
    assert protoc(old,kind,data=baseline) == protoc(current,kind,data=baseline), 'legacy decode changed'
new=protoc(current,'ControlMessage',text='sequence:4 rule_command{phase:"prepare" task_key:"key" target_revision:2}')
decoded=protoc(old,'ControlMessage',data=new).decode()
assert 'config_update' not in decoded and '18 {' in decoded, 'new rule body aliases legacy auto-apply body'
print('Historical CR-005/CR-006/CR-007 exact approvals, frozen hashes and unchanged legacy wire/schema passed')
