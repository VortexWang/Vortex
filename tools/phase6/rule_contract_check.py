"""Exact candidate review checks; never rewrite the approved freeze record."""
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
assert proposal['status'] == 'pending-owner-approval', 'candidate checker requires an unsigned proposal'
for row in proposal['artifacts']:
    assert hashlib.sha256((ROOT / row['path']).read_bytes()).hexdigest() == row['sha256'], 'candidate hash changed: '+row['path']
schema = load('contracts/runtime/config.schema.json')
assert schema['$defs']['legacy'] == json.loads(git_bytes('contracts/runtime/config.schema.json')), 'legacy schema changed'
assert (ROOT / 'docs/governance/contract-review.yaml').read_bytes() == git_bytes('docs/governance/contract-review.yaml'), 'historical owner review changed without separate approval'
assert {x['id']:x['version'] for x in load('contracts/versions.yaml')['contracts']} | {} == proposal['candidate_versions'], 'candidate catalog mismatch'
for row in load('contracts/versions.yaml')['contracts']:
    if row['id'] in {c['contract_id'] for c in proposal['changes']}:
        assert row['status'] == 'candidate', 'unsigned candidate marked frozen'
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
print('Exact CR-005 candidate hashes and legacy wire/schema compatibility passed')
