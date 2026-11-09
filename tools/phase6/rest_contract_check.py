#!/usr/bin/env python3
"""Validate the exact CR-009 review unit and independent owner freeze record."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = '4dc91a55c2fbe99fa8f183af993b7386805b691c'
SOURCE = '97966a3a70bff79afd43078ceadf211fe877a6d5'
REVIEWED_SOURCE = '5629543f6c83abf222d567bf9cdfc1b2e4f3a24d'
NOTICE = '\n## Phase 6 REST and durable events (06-07 approved)\n\nThe owner explicitly approved CR-009 with “批准 CR-009”. The immutable proposal\nSHA-256 remains `ed7ca2f0cf68f46d29984b7ddafbc13fc8de4ad171cd3378b0b9dda8e45bdfe0`.\nApproval is recorded in `docs/changes/CR-009-APPROVAL.json` and two appended\n`docs/governance/contract-review.yaml` records. Public API 3.0.0 is a breaking\nrelease from 2.3.0; database 2.4.0 is an additive release from 2.3.0.\nAll 99 candidate artifacts are bound to the clean tested source 97966a3a;\nonly catalog freeze metadata and this appended notice differ from candidate bytes.\nThe proposal, historical CR approvals, migrations 1–8 and other frozen contracts\nremain unchanged. Existing pending text is historical candidate metadata.\nFinal same-SHA Docker/Actions and cross-domain acceptance remain 06-11.\n'
PROPOSAL_SHA = 'ed7ca2f0cf68f46d29984b7ddafbc13fc8de4ad171cd3378b0b9dda8e45bdfe0'
CHANGED = {'vortex.database': ('2.3.0', '2.4.0', 'compatible'), 'vortex.public-api': ('2.3.0', '3.0.0', 'breaking')}
MUTABLE = {'contracts/database/schema.sql', 'contracts/openapi/v1/openapi.yaml', 'backend/openapi/v1/openapi.yaml', 'contracts/versions.yaml', 'contracts/CHANGELOG.md', 'contracts/requirements-map.yaml'}

def digest(raw): return hashlib.sha256(raw).hexdigest()
def historical(path):
    p = subprocess.run(['git', 'show', BASE+':'+path], cwd=ROOT, capture_output=True)
    return p.stdout if p.returncode == 0 else None
def load(path): return json.loads((ROOT/path).read_text())
def expected_paths():
    prior = json.loads(historical('docs/changes/CR-008-limits-runtime.json'))
    paths = {r['path'] for r in prior['artifacts']}
    for folder, pattern in [('backend/db/migrations','*.sql'), ('backend/db/query','*.sql'), ('backend/db/generated','*.go')]:
        paths.update(str(p.relative_to(ROOT)) for p in (ROOT/folder).glob(pattern))
        paths.update(subprocess.check_output(['git','ls-tree','-r','--name-only',BASE,'--',folder],cwd=ROOT,text=True).splitlines())
    paths.add('backend/db/sqlc.yaml')
    return paths

def check():
    proposal = load('docs/changes/CR-009-rest-events.json')
    assert digest((ROOT/'docs/changes/CR-009-rest-events.json').read_bytes())==PROPOSAL_SHA, 'reviewed proposal changed'
    assert proposal['id']=='CR-009' and proposal['plan']=='06-07' and proposal['status']=='pending-owner-approval'
    assert proposal['baseline_source']==BASE and proposal['acceptance']['owner_approval'] is False
    approved=(ROOT/'docs/changes/CR-009-APPROVAL.json').exists()
    if not approved:
        assert (ROOT/'docs/governance/contract-review.yaml').read_bytes()==historical('docs/governance/contract-review.yaml'), 'unapproved governance edit'
    for path in filter(None, subprocess.check_output(['git','ls-tree','-r','-z','--name-only',BASE,'--','docs/changes'],cwd=ROOT).decode().split('\0')):
        assert (ROOT/path).read_bytes()==historical(path), 'historical change/approval rewritten: '+path
    assert proposal['prior_approval']=={'path':'docs/changes/CR-008-APPROVAL.json','sha256':digest(historical('docs/changes/CR-008-APPROVAL.json')),'proposal_sha256':digest(historical('docs/changes/CR-008-limits-runtime.json'))}
    assert proposal['changes']==[dict(contract_id=k, **{'from':v[0]}, version=v[1], kind=v[2]) for k,v in CHANGED.items()]
    expected=expected_paths();rows=proposal['artifacts']
    assert len(rows)==len(expected) and {r['path'] for r in rows}==expected, 'incomplete/duplicate candidate inventory'
    prior=load('docs/changes/CR-008-limits-runtime.json')
    protected={r['path'] for r in prior['artifacts']}-MUTABLE
    for row in rows:
        raw=(ROOT/row['path']).read_bytes();old=historical(row['path'])
        if approved and row['path']=='contracts/versions.yaml':
            thawed=json.loads(raw)
            for entry in thawed['contracts']:
                if entry['id'] in CHANGED:
                    assert entry['status']=='frozen'
                    entry['status']='candidate'
            raw=(json.dumps(thawed,indent=2,ensure_ascii=False)+'\n').encode()
        elif approved and row['path']=='contracts/CHANGELOG.md':
            candidate=subprocess.check_output(['git','show',SOURCE+':'+row['path']],cwd=ROOT)
            assert raw==candidate+NOTICE.encode(), 'changelog exceeds exact approval notice'
            raw=candidate
        assert digest(raw)==row['sha256'], 'candidate drift: '+row['path']
        assert row['baseline_sha256']==(digest(old) if old is not None else None), 'baseline drift: '+row['path']
        if row['path'] in protected: assert raw==old, 'approved normative companion rewritten: '+row['path']
    catalog=load('contracts/versions.yaml');old_catalog=json.loads(historical('contracts/versions.yaml'))
    assert proposal['candidate_versions']=={r['id']:r['version'] for r in catalog['contracts']}
    for row in catalog['contracts']:
        old=next(r for r in old_catalog['contracts'] if r['id']==row['id'])
        if row['id'] in CHANGED:
            assert row['version']==CHANGED[row['id']][1] and row['status']==('frozen' if approved else 'candidate')
        else:
            assert row==old and (ROOT/row['path']).read_bytes()==historical(row['path']), 'unchanged frozen contract drift'
    if approved:
        approval=load('docs/changes/CR-009-APPROVAL.json')
        expected=dict(schema_version=1,id='CR-009',status='approved',proposal_path='docs/changes/CR-009-rest-events.json',proposal_sha256=PROPOSAL_SHA,owner_response='批准 CR-009',reviewer='user-approved-in-session',recorded_at='2026-11-09T00:53:26Z',candidate_source=SOURCE,reviewed_source=REVIEWED_SOURCE,candidate_catalog_sha256=next(r['sha256'] for r in rows if r['path']=='contracts/versions.yaml'),frozen_catalog_sha256=digest((ROOT/'contracts/versions.yaml').read_bytes()),changes=[])
        review=json.loads(historical('docs/governance/contract-review.yaml'))
        for change in proposal['changes']:
            entry=next(r for r in catalog['contracts'] if r['id']==change['contract_id'])
            record=dict(contract_id=entry['id'],**{'from':change['from']},version=change['version'],proposal_sha256=PROPOSAL_SHA,contract_sha256=digest((ROOT/entry['path']).read_bytes()),reviewer=expected['reviewer'],approved_at=expected['recorded_at'],decision='approved',approval_record='docs/changes/CR-009-APPROVAL.json')
            previous=next(r for r in review['contracts'] if r['id']==entry['id'])
            previous.update(version=entry['version'],sha256=record['contract_sha256'],decision='approved')
            review['change_approvals'].append(record);expected['changes'].append(record)
        assert approval==expected, 'exact owner/proposal/time/contract approval mismatch'
        assert load('docs/governance/contract-review.yaml')==review, 'exact historical governance append mismatch'
        for contract_id in ('vortex.database','vortex.public-api','vortex.control.v1','vortex.runtime-config','vortex.authorization.v1'):
            subprocess.run(['python3','tools/phase1/contracts.py','check-frozen','--contract-id',contract_id],cwd=ROOT,check=True)
    migrations=sorted((ROOT/'backend/db/migrations').glob('*.sql'))
    assert len(migrations)==9
    for path in migrations[:8]: assert path.read_bytes()==historical(str(path.relative_to(ROOT))), 'migration1..8 drift'
    parts=[f'-- Phase 6 implemented PostgreSQL schema through migration {migrations[-1].stem}.\n-- Canonical executable source: backend/db/migrations/*.sql.\n']
    parts += ['\n'.join(l for l in p.read_text().split('-- +goose Down',1)[0].splitlines() if not l.startswith('-- +goose'))+'\n' for p in migrations]
    assert (ROOT/'contracts/database/schema.sql').read_text()=='\n'.join(parts), 'schema projection drift'
    api=(ROOT/'contracts/openapi/v1/openapi.yaml').read_bytes()
    assert api==(ROOT/'backend/openapi/v1/openapi.yaml').read_bytes(), 'OpenAPI copy drift'
    assert json.loads(api)['info']['version']=='3.0.0'
    acceptance=proposal['acceptance']
    assert acceptance['clean_source_result']=='passed'
    source=acceptance['clean_source']
    assert source==SOURCE
    for row in rows:
        tested=subprocess.check_output(['git','show',source+':'+row['path']],cwd=ROOT)
        assert digest(tested)==row['sha256'], 'candidate differs from clean tested source: '+row['path']
    spec=importlib.util.spec_from_file_location('rest_evidence_gate',Path(__file__).with_name('rest_evidence_check.py'))
    gate=importlib.util.module_from_spec(spec);spec.loader.exec_module(gate)
    evidence=ROOT/acceptance['evidence']
    actual=gate.check(evidence/'docker',source)
    assert actual==json.loads((evidence/'clean-docker-validation.json').read_text()), 'clean evidence drift'
    summary=acceptance['validation_summary']
    for kind in ['normal','race']:
        assert summary[kind+'_test_events']==actual['tests'][kind]['passed_test_events']
    for args in [('check-map',),('check-change','--base',BASE)]: subprocess.run(['python3','tools/phase1/contracts.py',*args],cwd=ROOT,check=True)
    subprocess.run(['ruby','tools/phase6/openapi_validate.rb','contracts/openapi/v1/openapi.yaml'],cwd=ROOT,check=True)
    print(f'CR-009 exact review passed: {len(rows)} artifacts; original approvals protected; owner approval='+('approved' if approved else 'pending'))

if __name__=='__main__': check()
