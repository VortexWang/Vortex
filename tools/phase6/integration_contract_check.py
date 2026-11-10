#!/usr/bin/env python3
"""Check the additive CR-012 union without treating historical approval as current."""
import argparse
import hashlib
import json
import subprocess
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MAIN = '729cd8de28c115a2c5aa76b528100a26de4bbf0c'
PHASE6 = 'c85ed753c8ea92965346f12f5b4e7ea30e900695'
PROPOSAL = 'docs/governance/proposals/CR-012-phase6-integration.json'
APPROVAL = 'docs/changes/CR-012-APPROVAL.json'
MUTABLE = {'contracts/versions.yaml', 'contracts/CHANGELOG.md', 'contracts/requirements-map.yaml', 'contracts/protobuf/v1/vortex.proto', 'contracts/protobuf/v1/vortex.pb.go', 'contracts/protobuf/v1/lifecycle.md'}

def sha(raw): return hashlib.sha256(raw).hexdigest()
def historic(commit, path):
    return subprocess.check_output(['git','show',commit+':'+path],cwd=ROOT,stderr=subprocess.DEVNULL)
def load(path): return json.loads((ROOT/path).read_text())

def fields(raw):
    """Decode protobuf wire fields, retaining exact scalar/opaque option bytes."""
    pos=0;result={}
    def varint():
        nonlocal pos
        value=shift=0
        while True:
            b=raw[pos];pos+=1;value|=(b&127)<<shift
            if b<128:return value
            shift+=7
            assert shift<70, 'oversized descriptor varint'
    while pos<len(raw):
        tag=varint();number,kind=tag>>3,tag&7
        if kind==0:value=varint()
        elif kind==2:
            length=varint();value=raw[pos:pos+length];pos+=length
        else:raise AssertionError('unexpected descriptor wire type')
        result.setdefault(number,[]).append(value)
    assert pos==len(raw)
    return result

def descriptors(raw):
    f=fields(fields(raw)[1][0]);out={}
    def enum(raw,prefix):
        e=fields(raw);name=prefix+'.'+e[1][0].decode()
        out[name]=('enum',{fields(v)[1][0]:fields(v)[2][0] for v in e.get(2,[])})
    def message(raw,prefix):
        m=fields(raw);name=prefix+'.'+m[1][0].decode()
        # Every field descriptor includes name, number, type, label, oneof,
        # json name, options and proto3 optional. Preserve all exact bytes.
        out[name]=('message',{fields(v)[3][0]:v for v in m.get(2,[])})
        out[name+'.oneofs']=('oneofs',{i:v for i,v in enumerate(m.get(8,[]))})
        for e in m.get(4,[]):enum(e,name)
        for child in m.get(3,[]):message(child,name)
    prefix=f[2][0].decode()
    for m in f.get(4,[]):message(m,prefix)
    for e in f.get(5,[]):enum(e,prefix)
    for service in f.get(6,[]):
        s=fields(service);out[prefix+'.'+s[1][0].decode()]=('service',{fields(m)[1][0]:m for m in s.get(2,[])})
    return out

def check_wire():
    with tempfile.TemporaryDirectory(prefix='vortex-cr012-wire-') as directory:
        work=Path(directory);versions={}
        for name,commit in [('main',MAIN),('phase6',PHASE6),('candidate',None)]:
            folder=work/name;folder.mkdir();source=folder/'vortex.proto'
            source.write_bytes(historic(commit,'contracts/protobuf/v1/vortex.proto') if commit else (ROOT/'contracts/protobuf/v1/vortex.proto').read_bytes())
            subprocess.run(['protoc','-I',str(folder),'--descriptor_set_out='+str(folder/'wire.pb'),str(source)],check=True)
            versions[name]=descriptors((folder/'wire.pb').read_bytes())
        candidate=versions['candidate'];union={}
        for baseline in ['main','phase6']:
            for name,(kind,old) in versions[baseline].items():
                assert name in candidate and candidate[name][0]==kind, 'removed descriptor '+name
                for key,value in old.items():
                    assert candidate[name][1].get(key)==value, 'changed existing descriptor '+name+'/'+str(key)
                entry=union.setdefault(name,(kind,{}))
                for key,value in old.items():
                    assert key not in entry[1] or entry[1][key]==value, 'baseline field collision'
                    entry[1][key]=value
        assert candidate==union, 'candidate exceeds exact additive descriptor union'
        return {name:len(value) for name,value in versions.items()}

def check(require_approval=False):
    p=load(PROPOSAL)
    assert {r['path'] for r in p['review_files']}=={'docs/governance/proposals/CR-012-phase6-integration.md','docs/governance/proposals/CR-012-main.diff','docs/governance/proposals/CR-012-phase6.diff'} and len(p['review_files'])==3, 'incomplete review file inventory'
    for row in p['review_files']:
        assert sha((ROOT/row['path']).read_bytes())==row['sha256'], 'review file drift '+row['path']
    assert p['id']=='CR-012' and p['status']=='candidate-pending-exact-owner-approval'
    assert p['baselines']=={'main':MAIN,'phase6':PHASE6}
    assert p['change']=={'contract_id':'vortex.control.v1','from':'1.7.0','version':'1.8.0','kind':'compatible'}
    approved=(ROOT/APPROVAL).exists()
    catalog=load('contracts/versions.yaml')
    control=next(r for r in catalog['contracts'] if r['id']=='vortex.control.v1')
    assert control['version']=='1.8.0' and control['status']==('frozen' if approved else 'candidate')
    rows=p['artifacts'];expected=set(MUTABLE)|{'contracts/protobuf/v1/vortex_grpc.pb.go'}
    for base in [MAIN,PHASE6]:
        old=json.loads(historic(base,'contracts/versions.yaml'))
        for contract in old['contracts']:
            expected.add(contract['path']);expected.update(contract.get('artifacts',[]))
    assert len(rows)==len(expected) and {r['path'] for r in rows}==expected,'incomplete or duplicate artifact inventory'
    for row in rows:
        path=row['path'];raw=(ROOT/path).read_bytes()
        if approved and path=='contracts/versions.yaml':
            value=json.loads(raw);next(r for r in value['contracts'] if r['id']=='vortex.control.v1')['status']='candidate'
            raw=(json.dumps(value,ensure_ascii=False,indent=2)+'\n').encode()
        assert sha(raw)==row['sha256'],'candidate drift '+path
        for name,commit in p['baselines'].items():
            try:old=historic(commit,path)
            except subprocess.CalledProcessError:old=None
            assert row['baseline_sha256'][name]==(sha(old) if old is not None else None),'baseline drift '+path
        if path not in MUTABLE:
            commit=MAIN if path=='contracts/vlink/v1/encoding.md' else PHASE6
            assert raw==historic(commit,path),'protected normative artifact changed '+path
    # Freeze records keep both branches' complete prior approval rows intact.
    review=load('docs/governance/contract-review.yaml')
    prior=json.loads(historic(PHASE6,'docs/governance/contract-review.yaml'))
    other=json.loads(historic(MAIN,'docs/governance/contract-review.yaml'))
    combined=prior['change_approvals']+[r for r in other['change_approvals'] if r not in prior['change_approvals']]
    assert review['change_approvals'][:len(combined)]==combined,'historical approval rows changed'
    expected_history=set()
    for base in [MAIN,PHASE6]:
        expected_history.update(filter(None,subprocess.check_output(['git','ls-tree','-r','-z','--name-only',base,'--','docs/changes','docs/governance/proposals'],cwd=ROOT).decode().split('\0')))
    assert {r['path'] for r in p['historical_records']}==expected_history and len(p['historical_records'])==len(expected_history), 'incomplete historical approval inventory'
    for row in p['historical_records']:
        assert row['source'] in [MAIN,PHASE6]
        assert sha((ROOT/row['path']).read_bytes())==row['sha256']==sha(historic(row['source'],row['path'])),'historical review changed '+row['path']
    for contract in catalog['contracts']:
        if contract['id']=='vortex.control.v1':continue
        base=MAIN if contract['id']=='vortex.vlink.v1' else PHASE6
        previous=next(r for r in json.loads(historic(base,'contracts/versions.yaml'))['contracts'] if r['id']==contract['id'])
        assert contract==previous,'unrelated catalog change'
        subprocess.run(['python3','tools/phase1/contracts.py','check-frozen','--contract-id',contract['id']],cwd=ROOT,check=True,stdout=subprocess.DEVNULL)
    if not approved:
        assert sha((ROOT/'docs/governance/contract-review.yaml').read_bytes())==p['candidate_review_sha256'], 'candidate review drift'
    assert load('contracts/requirements-map.yaml')['integration_review']['change_request']=='CR-012'
    for args in [('check-map',),('check-change','--base',MAIN),('check-change','--base',PHASE6)]:
        subprocess.run(['python3','tools/phase1/contracts.py',*args],cwd=ROOT,check=True,stdout=subprocess.DEVNULL)
    wire=check_wire()
    if approved:
        a=load(APPROVAL)
        assert a['id']=='CR-012' and a['status']=='approved' and a['owner_response']=='批准 CR-012'
        assert a['proposal_path']==PROPOSAL and a['proposal_sha256']==sha((ROOT/PROPOSAL).read_bytes())
        assert len(review['change_approvals'])==len(combined)+1 and review['change_approvals'][-1]==a['change']
        assert a['change']['approval_record']==APPROVAL and a['change']['proposal_sha256']==a['proposal_sha256']
        assert a['reviewer']=='user-approved-in-session' and a['recorded_at']
        expected_change=dict(contract_id='vortex.control.v1', **{'from':'1.7.0'}, version='1.8.0', proposal_sha256=a['proposal_sha256'], contract_sha256=sha((ROOT/control['path']).read_bytes()), reviewer=a['reviewer'], approved_at=a['recorded_at'], decision='approved', approval_record=APPROVAL, artifacts=[dict(path=path,sha256=sha((ROOT/path).read_bytes())) for path in control['artifacts']])
        assert a['change']==expected_change,'exact current approval record mismatch'
        expected_review=json.loads(historic(PHASE6,'docs/governance/contract-review.yaml'))
        vlink=next(r for r in other['contracts'] if r['id']=='vortex.vlink.v1')
        next(r for r in expected_review['contracts'] if r['id']=='vortex.vlink.v1').update(vlink)
        next(r for r in expected_review['contracts'] if r['id']=='vortex.control.v1').update(version='1.8.0',sha256=expected_change['contract_sha256'],artifacts=expected_change['artifacts'],decision='approved')
        expected_review['change_approvals']=combined+[expected_change]
        assert review==expected_review,'approval exceeds exact permitted reconciliation'
        subprocess.run(['python3','tools/phase1/contracts.py','check-frozen'],cwd=ROOT,check=True)
    else:
        assert len(review['change_approvals'])==len(combined)
        assert not require_approval,'CR-012 exact owner approval is pending'
    print('CR-012 exact additive integration passed; descriptors='+json.dumps(wire)+'; owner approval='+('approved' if approved else 'pending'))

if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--require-approval',action='store_true');args=parser.parse_args()
    check(args.require_approval)
