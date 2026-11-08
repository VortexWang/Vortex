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
approval_path = ROOT / 'docs/changes/CR-008-APPROVAL.json'
approved = approval_path.exists()
original_review = json.loads(historical('docs/governance/contract-review.yaml'))
current_review = load('docs/governance/contract-review.yaml')
if not approved:
    assert current_review == original_review, 'unapproved governance edit'
for path in [str(p.relative_to(ROOT)) for p in (ROOT / 'docs/changes').glob('CR-00[567]*')]:
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
    raw = (ROOT / path).read_bytes()
    if approved and path == 'contracts/versions.yaml':
        thawed = json.loads(raw)
        for entry in thawed['contracts']:
            if entry['id'] in {c['contract_id'] for c in proposal['changes']}:
                assert entry['status'] == 'frozen'
                entry['status'] = 'candidate'
        raw = (json.dumps(thawed, indent=2, ensure_ascii=False) + '\n').encode()
    elif approved and path == 'contracts/CHANGELOG.md':
        candidate_raw = subprocess.check_output(['git', 'show', 'cf92b7e24b6c557a5d14a18b0549a67eac349eb4:' + path], cwd=ROOT)
        notice = (ROOT / 'artifacts/phase6/06-06/approval/changelog-notice.txt').read_bytes()
        assert raw == candidate_raw + notice, 'unapproved changelog append'
        raw = candidate_raw
    assert sha(raw) == row['sha256'], 'candidate changed: ' + path
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
        assert row['status'] == ('frozen' if approved else 'candidate')
    else:
        assert row == old and (ROOT / row['path']).read_bytes() == historical(row['path']), 'unchanged frozen contract drift'
if approved:
    approval = json.loads(approval_path.read_text())
    proposal_hash = sha((ROOT / 'docs/changes/CR-008-limits-runtime.json').read_bytes())
    assert approval['id'] == 'CR-008' and approval['status'] == 'approved'
    assert approval['proposal_path'] == 'docs/changes/CR-008-limits-runtime.json' and approval['proposal_sha256'] == proposal_hash == '4e29497f82808b9a8693030ef400dae3a2bc42cc27cf8a39f24bdcf8605074bd'
    assert approval['owner_response'] == '\u6279\u51c6 CR-008' and approval['reviewer'] == 'user-approved-in-session'
    assert approval['candidate_source'] == 'cf92b7e24b6c557a5d14a18b0549a67eac349eb4'
    assert approval['frozen_catalog_sha256'] == sha((ROOT / 'contracts/versions.yaml').read_bytes())
    assert approval['candidate_catalog_sha256'] == next(r['sha256'] for r in proposal['artifacts'] if r['path'] == 'contracts/versions.yaml')
    expected_review = json.loads(json.dumps(original_review))
    expected_changes = []
    for change in proposal['changes']:
        entry = next(r for r in catalog['contracts'] if r['id'] == change['contract_id'])
        record = dict(contract_id=entry['id'], **{'from': change['from']}, version=change['version'], proposal_sha256=proposal_hash, contract_sha256=sha((ROOT / entry['path']).read_bytes()), reviewer='user-approved-in-session', approved_at=approval['recorded_at'], decision='approved', approval_record='docs/changes/CR-008-APPROVAL.json')
        if entry.get('artifacts'):
            record['artifacts'] = [dict(path=p, sha256=sha((ROOT / p).read_bytes())) for p in entry['artifacts']]
        old = next(r for r in expected_review['contracts'] if r['id'] == entry['id'])
        old.update(version=entry['version'], sha256=record['contract_sha256'], decision='approved')
        if 'artifacts' in record:
            old['artifacts'] = record['artifacts']
        expected_review['change_approvals'].append(record)
        expected_changes.append(record)
    assert approval['changes'] == expected_changes, 'exact approval correlation/companions mismatch'
    assert current_review == expected_review, 'exact governance append mismatch'
    for contract_id in ('vortex.database', 'vortex.public-api', 'vortex.control.v1', 'vortex.runtime-config', 'vortex.authorization.v1'):
        subprocess.run(['python3', 'tools/phase1/contracts.py', 'check-frozen', '--contract-id', contract_id], cwd=ROOT, check=True)
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
print('CR-008 candidate: exact inventory, prior approval protection, unchanged frozen schemas and legacy wire passed; approval=' + ('approved' if approved else 'pending'))
