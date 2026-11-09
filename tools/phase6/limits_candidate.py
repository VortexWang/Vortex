"""Build the exact CR-008 review inventory; never create owner approval."""
import hashlib
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = '84821a04cec4dec78a8a3b192d2003d1f44518bc'


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def dump(path, value):
    (ROOT / path).write_text(json.dumps(value, indent=2, ensure_ascii=False) + '\n')


def historical(path):
    result = subprocess.run(['git', 'show', BASE + ':' + path], cwd=ROOT, capture_output=True)
    return result.stdout if result.returncode == 0 else None


def main():
    catalog = json.loads((ROOT / 'contracts/versions.yaml').read_text())
    changes = []
    for entry in catalog['contracts']:
        if entry['id'] in ('vortex.database', 'vortex.public-api', 'vortex.control.v1'):
            before, after = ('1.5.0', '1.6.0') if entry['id'] == 'vortex.control.v1' else ('2.2.0', '2.3.0')
            entry.update(version=after, status='candidate')
            changes.append(dict(contract_id=entry['id'], **{'from': before}, version=after, kind='compatible'))
        if entry['id'] == 'vortex.control.v1':
            path = 'contracts/protobuf/v1/share-handoff.schema.json'
            if path not in entry['artifacts']:
                entry['artifacts'].append(path)
    dump('contracts/versions.yaml', catalog)
    mapping = json.loads((ROOT / 'contracts/requirements-map.yaml').read_text())
    for row in mapping['requirements']:
        if row['id'].startswith('LIM-'):
            row.update(implementation_plan='06-06', contract_candidate='CR-008',
                       verification=['docs/coverage/phase6/LIMITS.md', 'tools/phase6/share_fault_injection.sh'],
                       http_adapter_plan='06-07')
    dump('contracts/requirements-map.yaml', mapping)
    path = ROOT / 'contracts/CHANGELOG.md'
    text = path.read_text()
    match = re.search(r'```json\s*(\{.*?\})\s*```', text, re.S)
    record = json.loads(match.group(1))
    for change in changes:
        record['changes'] = [r for r in record['changes'] if r['contract_id'] != change['contract_id']]
        record['changes'].append({**change, 'status': 'pending-owner-approval',
                                  'summary': 'CR-008 finite source-funded Hub runtime, independent share/lease/credit closure and explicit fenced recovery.'})
    text = text[:match.start(1)] + json.dumps(record, separators=(',', ':')) + text[match.end(1):]
    if '## Phase 6 limits and funded runtime (06-06 candidate)' not in text:
        text += '''\n## Phase 6 limits and funded runtime (06-06 candidate)

CR-008 proposes database/API 2.3.0 and control 1.6.0 as compatible extensions.
Shares, runtime leases and source-funded credit remain independent rights.
Renewal/closure, complete inventory, original-source finalization and explicit
Node/Hub boot recovery require their own durable authenticated proofs. Unknown
liabilities remain held. Negotiated capabilities isolate new commands/journals;
existing field numbers and old wire encodings remain unchanged. Runtime,
authorization and VLink frozen schemas retain their approved bytes/versions.
No owner approval is implied; HTTP adapters remain06-07, paired acceptance06-11.
'''
    path.write_text(text)
    previous = json.loads((ROOT / 'docs/changes/CR-007-metering-ledger.json').read_text())
    paths = {r['path'] for r in previous['artifacts']}
    paths.add('backend/db/migrations/00008_limits_shares.sql')
    paths.add('contracts/protobuf/v1/share-handoff.schema.json')
    # Wire payload codecs are included explicitly in this review inventory.
    paths.update(str(p.relative_to(ROOT)) for p in (ROOT / 'contracts').glob('*.go') if not p.name.endswith('_test.go'))
    artifacts = []
    for path in sorted(paths):
        old = historical(path)
        artifacts.append(dict(path=path, sha256=digest((ROOT / path).read_bytes()),
                              baseline_sha256=digest(old) if old is not None else None))
    approval = 'docs/changes/CR-007-APPROVAL.json'
    dump('docs/changes/CR-008-limits-runtime.json', dict(
        schema_version=1, id='CR-008', status='pending-owner-approval', created='2026-11-08', plan='06-06',
        baseline_source=BASE,
        prior_approval=dict(path=approval, sha256=digest((ROOT / approval).read_bytes()),
                            proposal_sha256=digest((ROOT / 'docs/changes/CR-007-metering-ledger.json').read_bytes())),
        changes=changes, candidate_versions={c['id']: c['version'] for c in catalog['contracts']}, artifacts=artifacts,
        acceptance=dict(owner_approval=False, http_adapter_plan='06-07', paired_acceptance_plan='06-11',
                        scope='06-06 producer and bounded race/fault evidence; no all-member forwarding/restart or physical power-loss claim')))
    print('CR-008 candidate SHA-256:', digest((ROOT / 'docs/changes/CR-008-limits-runtime.json').read_bytes()))


if __name__ == '__main__':
    main()
