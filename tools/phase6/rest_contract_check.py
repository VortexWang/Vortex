#!/usr/bin/env python3
"""Validate exact CR-009 candidate inventory without granting owner approval."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASE = '4dc91a55c2fbe99fa8f183af993b7386805b691c'
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
    assert proposal['id']=='CR-009' and proposal['plan']=='06-07' and proposal['status']=='pending-owner-approval'
    assert proposal['baseline_source']==BASE and proposal['acceptance']['owner_approval'] is False
    assert not (ROOT/'docs/changes/CR-009-APPROVAL.json').exists(), 'candidate checker never grants/validates a new approval'
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
        assert digest(raw)==row['sha256'], 'candidate drift: '+row['path']
        assert row['baseline_sha256']==(digest(old) if old is not None else None), 'baseline drift: '+row['path']
        if row['path'] in protected: assert raw==old, 'approved normative companion rewritten: '+row['path']
    catalog=load('contracts/versions.yaml');old_catalog=json.loads(historical('contracts/versions.yaml'))
    assert proposal['candidate_versions']=={r['id']:r['version'] for r in catalog['contracts']}
    for row in catalog['contracts']:
        old=next(r for r in old_catalog['contracts'] if r['id']==row['id'])
        if row['id'] in CHANGED:
            assert row['version']==CHANGED[row['id']][1] and row['status']=='candidate'
        else:
            assert row==old and (ROOT/row['path']).read_bytes()==historical(row['path']), 'unchanged frozen contract drift'
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
    print(f'CR-009 exact candidate passed: {len(rows)} artifacts; original approvals protected; owner approval remains pending')

if __name__=='__main__': check()
