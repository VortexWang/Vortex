"""Check CR-008 candidate bytes and historical approvals without approving it."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
BASE = '84821a04cec4dec78a8a3b192d2003d1f44518bc'


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def historical(path):
    p = subprocess.run(['git', 'show', BASE + ':' + path], cwd=ROOT, capture_output=True)
    return p.stdout if p.returncode == 0 else None


def load(path):
    return json.loads((ROOT / path).read_text())


proposal = load('docs/changes/CR-008-limits-runtime.json')
assert proposal['id'] == 'CR-008' and proposal['plan'] == '06-06'
assert proposal['status'] == 'pending-owner-approval' and proposal['acceptance']['owner_approval'] is False
assert proposal['baseline_source'] == BASE
assert not (ROOT / 'docs/changes/CR-008-APPROVAL.json').exists(), 'candidate validation cannot manufacture approval'
for path in ['docs/governance/contract-review.yaml'] + [str(p.relative_to(ROOT)) for p in (ROOT / 'docs/changes').glob('CR-00[567]*')]:
    assert (ROOT / path).read_bytes() == historical(path), 'historical approval/review changed: ' + path
prior = proposal['prior_approval']
assert prior['path'] == 'docs/changes/CR-007-APPROVAL.json'
assert prior['sha256'] == sha((ROOT / prior['path']).read_bytes())
assert prior['proposal_sha256'] == sha((ROOT / 'docs/changes/CR-007-metering-ledger.json').read_bytes())
previous = load('docs/changes/CR-007-metering-ledger.json')
required = {r['path'] for r in previous['artifacts']} | {'backend/db/migrations/00008_limits_shares.sql', 'contracts/protobuf/v1/share-handoff.schema.json'}
required.update(str(p.relative_to(ROOT)) for p in (ROOT / 'contracts').glob('*.go') if not p.name.endswith('_test.go'))
assert len(proposal['artifacts']) == len(required) and {r['path'] for r in proposal['artifacts']} == required
for row in proposal['artifacts']:
    path = row['path']
    assert sha((ROOT / path).read_bytes()) == row['sha256'], 'candidate changed: ' + path
    old = historical(path)
    assert row['baseline_sha256'] == (sha(old) if old is not None else None), 'baseline changed: ' + path
for path in sorted((ROOT / 'backend/db/migrations').glob('0000[1-7]*.sql')):
    assert path.read_bytes() == historical(str(path.relative_to(ROOT))), 'approved migration rewritten'
catalog = load('contracts/versions.yaml')
assert proposal['candidate_versions'] == {r['id']: r['version'] for r in catalog['contracts']}
old_catalog = json.loads(historical('contracts/versions.yaml'))
expected = [('vortex.database', '2.2.0', '2.3.0'), ('vortex.public-api', '2.2.0', '2.3.0'), ('vortex.control.v1', '1.5.0', '1.6.0')]
assert proposal['changes'] == [dict(contract_id=c, **{'from': a}, version=b, kind='compatible') for c, a, b in expected]
for row in catalog['contracts']:
    old = next(r for r in old_catalog['contracts'] if r['id'] == row['id'])
    if row['id'] in {r[0] for r in expected}:
        assert row['status'] == 'candidate'
    else:
        assert row == old and (ROOT / row['path']).read_bytes() == historical(row['path']), 'unchanged frozen contract drift'
parts = [f'-- Phase 6 implemented PostgreSQL schema through migration 00008_limits_shares.\n-- Canonical executable source: backend/db/migrations/*.sql.\n']
for path in sorted((ROOT / 'backend/db/migrations').glob('*.sql')):
    parts.append('\n'.join(l for l in path.read_text().split('-- +goose Down', 1)[0].splitlines() if not l.startswith('-- +goose')) + '\n')
assert (ROOT / 'contracts/database/schema.sql').read_text() == '\n'.join(parts), 'schema projection drift'
assert (ROOT / 'contracts/openapi/v1/openapi.yaml').read_bytes() == (ROOT / 'backend/openapi/v1/openapi.yaml').read_bytes()
work = Path(sys.argv[1])
old = work / 'old.proto'
old.write_bytes(historical('contracts/protobuf/v1/vortex.proto'))
new = ROOT / 'contracts/protobuf/v1/vortex.proto'
for kind, message in [
    ('NodeMessage', 'node_id:"node" boot_id:"boot" sequence:1 heartbeat{applied_config_version:7 observed_at:"2026-11-07T00:00:00Z"}'),
    ('ControlMessage', 'message_id:"message" sequence:2 config_update{version:7 payload:"{}" transaction_id:"transaction-0001" fencing_generation:3 payload_sha256:"digest" expected_version:6}'),
    ('ControlMessage', 'sequence:3 authorization_update{generation:5 payload:"{}"}'),
    ('ControlMessage', 'sequence:4 rule_command{phase:"prepare" task_key:"key" target_revision:2}'),
]:
    encoded = []
    for path in [old, new]:
        encoded.append(subprocess.check_output(['protoc', '-I', str(path.parent), '--encode=vortex.control.v1.' + kind, path.name], input=message.encode()))
    assert encoded[0] == encoded[1], 'legacy wire changed: ' + kind
    decoded = [subprocess.check_output(['protoc', '-I', str(path.parent), '--decode=vortex.control.v1.' + kind, path.name], input=encoded[0]) for path in [old, new]]
    assert decoded[0] == decoded[1], 'legacy decode changed: ' + kind
subprocess.run(['python3', 'tools/phase1/contracts.py', 'check-map'], cwd=ROOT, check=True)
subprocess.run(['python3', 'tools/phase1/contracts.py', 'check-change', '--base', BASE], cwd=ROOT, check=True)
print('CR-008 candidate: exact inventory, prior approval protection, unchanged frozen schemas and legacy wire passed; owner approval pending')
