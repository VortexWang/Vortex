"""Verify the exact owner-approved CR-005 review unit and legacy compatibility."""
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
def protoc(source, kind, text=None, data=None):
    args = ['protoc', '-I', str(source.parent), '--'+('encode' if text is not None else 'decode')+'=vortex.control.v1.'+kind, source.name]
    return subprocess.check_output(args, input=text.encode() if text is not None else data)

proposal = load('docs/changes/CR-005-rule-two-phase.json')
approval = load('docs/changes/CR-005-APPROVAL.json')
proposal_hash = hashlib.sha256((ROOT / 'docs/changes/CR-005-rule-two-phase.json').read_bytes()).hexdigest()
assert approval['status'] == 'approved' and approval['owner_response'] == '批准 CR-005'
assert approval['proposal_sha256'] == proposal_hash == 'b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4', 'approved proposal changed'
catalog = load('contracts/versions.yaml')
changes = {row['contract_id']: row for row in proposal['changes']}
for row in proposal['artifacts']:
    raw = (ROOT / row['path']).read_bytes()
    if row['path'] == 'contracts/versions.yaml':
        assert hashlib.sha256(raw).hexdigest() == approval['frozen_catalog_sha256'], 'frozen catalog changed'
        original_catalog = json.loads(raw)
        for entry in original_catalog['contracts']:
            if entry['id'] in changes:
                assert entry['status'] == 'frozen', 'approved contract is not frozen'
                entry['status'] = 'candidate'
        raw = (json.dumps(original_catalog, indent=2)+'\n').encode()
        assert row['sha256'] == approval['candidate_catalog_sha256']
    assert hashlib.sha256(raw).hexdigest() == row['sha256'], 'approved candidate hash changed: '+row['path']
schema = load('contracts/runtime/config.schema.json')
assert schema['$defs']['legacy'] == json.loads(git_bytes('contracts/runtime/config.schema.json')), 'legacy schema changed'
review = load('docs/governance/contract-review.yaml')
historical = json.loads(git_bytes('docs/governance/contract-review.yaml'))
old_approvals = historical['change_approvals']
assert review['change_approvals'][:len(old_approvals)] == old_approvals, 'historical approvals rewritten'
assert review['change_approvals'][len(old_approvals):] == approval['changes'], 'signed approval history mismatch'
assert len(approval['changes']) == len(changes), 'missing or duplicate approval'
for row in approval['changes']:
    change = changes[row['contract_id']]
    assert row['decision'] == 'approved' and row['proposal_sha256'] == proposal_hash
    assert row['from'] == change['from'] and row['version'] == change['version']
    assert row['reviewer'] == approval['reviewer'] and row['approved_at'] == approval['recorded_at']
    subprocess.run(['python3', 'tools/phase1/contracts.py', 'check-frozen', '--contract-id', row['contract_id']], cwd=ROOT, check=True)
assert {x['id']:x['version'] for x in catalog['contracts']} == proposal['candidate_versions'], 'approved catalog version mismatch'
assert (ROOT / 'contracts/openapi/v1/openapi.yaml').read_bytes() == (ROOT / 'backend/openapi/v1/openapi.yaml').read_bytes(), 'OpenAPI copy differs'
for path in sorted((ROOT / 'backend/db/migrations').glob('0000[1-4]*.sql')):
    assert path.read_bytes() == git_bytes(str(path.relative_to(ROOT))), 'applied migration rewritten: '+path.name
subprocess.run(['python3', 'tools/phase1/contracts.py', 'check-change', '--base', proposal['baseline_source']], cwd=ROOT, check=True)
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
print('Exact owner-approved CR-005 hashes, frozen records and legacy wire/schema compatibility passed')
