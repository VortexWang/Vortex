"""05-06 review fixtures only; no production validator or runtime qualification.

Uses stdlib plus the project's already pinned protoc. New schemas are exercised
for the keywords they use, and protobuf bytes are encoded/decoded by protoc.
"""
import copy
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
PHASE = ROOT / '.planning/phases/05-m3-agent-edge-hub-m3-agent-and-hub'
CANDIDATES = PHASE / '05-06-contract-candidates'
MAX = 9223372036854775807


def load(name):
    return json.loads((CANDIDATES / name).read_text())


def baseline_bytes(path):
    source=load('manifest.json')['baseline_source_sha']
    return subprocess.check_output(['git','show',source+':'+path],cwd=ROOT)


def timestamp(s):
    if not isinstance(s, str) or not re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z', s):
        raise ValueError('noncanonical UTC time')
    return dt.datetime.fromisoformat(s[:-1] + '+00:00')


def validate(value, schema, root=None):
    """Explicit fixture evaluator, not a complete JSON Schema implementation."""
    root = schema if root is None else root
    known = {'$schema', '$id', '$defs', '$ref', 'title', 'description', 'default',
             'type', 'const', 'enum', 'required', 'properties', 'additionalProperties',
             'items', 'minItems', 'maxItems', 'uniqueItems', 'minimum', 'maximum',
             'minLength', 'maxLength', 'pattern', 'format', 'allOf', 'anyOf',
             'oneOf', 'not', 'if', 'then', 'else', 'dependentRequired', 'x-vortex-source'}
    if set(schema) - known:
        raise ValueError('unsupported fixture schema keyword')
    if '$ref' in schema:
        if not schema['$ref'].startswith('#/$defs/'):
            raise ValueError('nonlocal schema reference')
        validate(value, root['$defs'][schema['$ref'].split('/')[-1]], root)
    def matches(s):
        try:
            validate(value, s, root)
            return True
        except ValueError:
            return False
    for s in schema.get('allOf', []):
        validate(value, s, root)
    for k in ('anyOf', 'oneOf'):
        if k in schema:
            count = sum(matches(s) for s in schema[k])
            if count == 0 or (k == 'oneOf' and count != 1):
                raise ValueError(k)
    if 'not' in schema and matches(schema['not']):
        raise ValueError('forbidden branch')
    if 'if' in schema:
        validate(value, schema.get('then' if matches(schema['if']) else 'else', {}), root)
    kind = schema.get('type')
    types = {'object': lambda v: isinstance(v, dict), 'array': lambda v: isinstance(v, list),
             'string': lambda v: isinstance(v, str), 'integer': lambda v: type(v) is int,
             'number': lambda v: type(v) in (float, int), 'boolean': lambda v: type(v) is bool}
    if kind and (kind not in types or not types[kind](value)):
        raise ValueError('type')
    if 'const' in schema and (value != schema['const'] or (isinstance(value, bool) != isinstance(schema['const'], bool))):
        raise ValueError('const')
    if 'enum' in schema and value not in schema['enum']:
        raise ValueError('enum')
    if isinstance(value, dict):
        if set(schema.get('required', [])) - value.keys():
            raise ValueError('required')
        props = schema.get('properties', {})
        if schema.get('additionalProperties') is False and set(value) - props.keys():
            raise ValueError('unknown field')
        for k, v in value.items():
            if k in props:
                validate(v, props[k], root)
        for k, required in schema.get('dependentRequired', {}).items():
            if k in value and set(required) - value.keys():
                raise ValueError('dependentRequired')
    if isinstance(value, list):
        if not schema.get('minItems', 0) <= len(value) <= schema.get('maxItems', MAX):
            raise ValueError('array bounds')
        if schema.get('uniqueItems') and len({json.dumps(v, sort_keys=True) for v in value}) != len(value):
            raise ValueError('duplicate item')
        for v in value:
            validate(v, schema.get('items', {}), root)
    if isinstance(value, str):
        if not schema.get('minLength', 0) <= len(value) <= schema.get('maxLength', MAX):
            raise ValueError('string bounds')
        if 'pattern' in schema and not re.search(schema['pattern'], value):
            raise ValueError('pattern')
        if schema.get('format') == 'date-time':
            timestamp(value)
        if schema.get('format') == 'uuid' and not re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', value):
            raise ValueError('uuid')
    if type(value) in (int, float) and not schema.get('minimum', -MAX) <= value <= schema.get('maximum', MAX):
        raise ValueError('numeric bounds')


def validate_chain(rows, anchors, issuer, node):
    """Review-model oracle; actual durable CAS/aggregate occupancy is 05-13."""
    schema = load('lifecycle.schema.json')
    index = {}
    for r in rows:
        validate(r, schema['$defs']['share_reference'], schema)
        if r['share_id'] in index:
            raise ValueError('duplicate share')
        index[r['share_id']] = r
    for r in rows:
        cursor, seen = r, set()
        while True:
            if cursor['share_id'] in seen or len(seen) >= 32:
                raise ValueError('cyclic/deep lineage')
            seen.add(cursor['share_id'])
            if cursor['allowed_issuer_hub_id'] != issuer or cursor['subject_node_id'] != node:
                raise ValueError('issuer/subject')
            if not timestamp(cursor['not_before']) < timestamp(cursor['expires_at']) <= timestamp(cursor['hard_deadline']):
                raise ValueError('share time')
            if not cursor['parent_share_id']:
                if cursor['parent_revision'] != 0 or anchors.get(cursor['share_id']) != cursor:
                    raise ValueError('untrusted source anchor')
                break
            parent = index.get(cursor['parent_share_id'])
            if parent is None or cursor['parent_revision'] != parent['revision']:
                raise ValueError('missing/stale parent')
            for k in ('source_id', 'rule_id', 'rule_revision', 'path_generation'):
                if cursor[k] != parent[k]:
                    raise ValueError('lineage scope')
            for k in ('rate_bps', 'connections'):
                if cursor[k] > parent[k]:
                    raise ValueError('parent amount')
            if timestamp(cursor['not_before']) < timestamp(parent['not_before']) or timestamp(cursor['expires_at']) > timestamp(parent['expires_at']) or timestamp(cursor['hard_deadline']) > timestamp(parent['hard_deadline']):
                raise ValueError('parent boundary')
            cursor = parent


def validate_policy(p, funded_ids):
    schema = load('lifecycle.schema.json')
    validate(p, schema['$defs']['policy_transition'], schema)
    old, new = p['old_snapshot'], p['new_snapshot']
    if old['snapshot_id'] == new['snapshot_id'] or new['policy_revision'] <= old['policy_revision']:
        raise ValueError('mutable snapshot')
    for k in ('rule_id', 'execution_node_id', 'path_generation'):
        if old[k] != new[k] or (k != 'execution_node_id' and p[k] != new[k]):
            raise ValueError('snapshot scope')
    for k in ('future_source_id', 'future_credit_authorization_id', 'future_runtime_authorization_id', 'future_share_authorization_id'):
        if p[k] not in funded_ids:
            raise ValueError('unfunded preissue')


def textproto(v):
    lines = []
    for k, value in v.items():
        for item in value if isinstance(value, list) else [value]:
            if isinstance(item, dict):
                lines.append(k + ' { ' + textproto(item) + ' }')
            elif k.endswith('sha256'):
                lines.append(k + ': "' + ''.join('\\x%02x' % b for b in bytes.fromhex(item)) + '"')
            elif isinstance(item, str):
                lines.append(k + ': ' + json.dumps(item))
            elif isinstance(item, bool):
                lines.append(k + ': ' + str(item).lower())
            else:
                lines.append(k + ': ' + str(item))
    return '\n'.join(lines)


class LifecycleCandidateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.schema = load('lifecycle.schema.json')
        cls.vectors = load('vectors.json')

    def test_manifest_and_frozen_baselines_match_exact_bytes(self):
        manifest = load('manifest.json')
        self.assertEqual(manifest['status'], 'candidate-awaiting-owner-approval')
        for name, digest in manifest['candidates'].items():
            self.assertEqual(hashlib.sha256((CANDIDATES / name).read_bytes()).hexdigest(), digest, name)
        for name, digest in manifest['baselines'].items():
            self.assertEqual(hashlib.sha256(baseline_bytes(name)).hexdigest(), digest, name)
        self.assertEqual(set(manifest['candidates']), {x.name for x in CANDIDATES.iterdir() if x.is_file() and x.name != 'manifest.json'})

    def test_positive_projection_json_round_trips(self):
        self.assertEqual(set(self.vectors), set(self.schema['$defs']))
        for name, value in self.vectors.items():
            with self.subTest(name=name):
                decoded = json.loads(json.dumps(value))
                validate(decoded, self.schema['$defs'][name], self.schema)
                self.assertEqual(decoded, value)

    def test_rejects_overflow_missing_unknown_and_mutated_identity(self):
        cases = [('requested_raw_bytes', MAX + 1), ('requested_raw_bytes', True),
                 ('requested_raw_bytes', -1), ('requested_ttl_seconds', 301),
                 ('node_id', 'bad-id'), ('node_id', self.vectors['renewal_request']['node_id'].upper()),
                 ('request_sha256', 'a' * 63), ('signer_epoch', 0), ('rule_id', 'x' * 129)]
        for k, v in cases:
            value = copy.deepcopy(self.vectors['renewal_request']); value[k] = v
            with self.subTest(field=k, value=v), self.assertRaises(ValueError):
                validate(value, self.schema['$defs']['renewal_request'], self.schema)
        for mutation in ('missing', 'unknown', 'null'):
            value = copy.deepcopy(self.vectors['renewal_request'])
            if mutation == 'missing': del value['source_id']
            elif mutation == 'unknown': value['target_address'] = '127.0.0.1'
            else: value['source_id'] = None
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                validate(value, self.schema['$defs']['renewal_request'], self.schema)

    def test_bounds_and_result_payload_presence(self):
        for name, field, bad in [('hello','capabilities',['renewal-v1'] * 7),('hello','capabilities',['unknown-v1']),('hub_status','mode','healthy'),('node_report','probes',[self.vectors['node_report']['probes'][0]] * 1025),('backend_heartbeat','observed_at','2026-11-06T00:00:00+00:00')]:
            value = copy.deepcopy(self.vectors[name]);value[field] = bad
            with self.subTest(name=name, field=field), self.assertRaises(ValueError):
                validate(value, self.schema['$defs'][name], self.schema)
        value = copy.deepcopy(self.vectors['renewal_result']);del value['authorization_update_sha256']
        with self.assertRaises(ValueError): validate(value,self.schema['$defs']['renewal_result'],self.schema)
        value['code']='quota_exhausted';validate(value,self.schema['$defs']['renewal_result'],self.schema)
        value['retryable']=True
        with self.assertRaises(ValueError): validate(value,self.schema['$defs']['renewal_result'],self.schema)
        value['retryable']=False
        value['authorization_update_sha256']='a'*64
        with self.assertRaises(ValueError): validate(value,self.schema['$defs']['renewal_result'],self.schema)

    def test_uninitialized_hub_is_blocked_not_online_authority(self):
        value=copy.deepcopy(self.vectors['hub_status'])
        value.update(mode='blocked',signer_epoch=0,backend_heartbeat_at='',autonomy_deadline='')
        validate(value,self.schema['$defs']['hub_status'],self.schema)
        value['mode']='online'
        with self.assertRaises(ValueError):validate(value,self.schema['$defs']['hub_status'],self.schema)

    def test_share_chain_rejects_stale_missing_cyclic_scope_and_untrusted_anchor(self):
        parent = copy.deepcopy(self.vectors['share_reference']);child=copy.deepcopy(parent)
        child.update(share_id='550e8400-e29b-41d4-a716-000000000021',parent_share_id=parent['share_id'],parent_revision=1)
        anchors={parent['share_id']:parent};issuer=parent['allowed_issuer_hub_id'];node=parent['subject_node_id']
        validate_chain([parent,child],anchors,issuer,node)
        for field, bad in [('parent_revision',2),('source_id',child['share_id']),('allowed_issuer_hub_id',child['share_id']),('rate_bps',parent['rate_bps']+1),('expires_at','2026-11-06T01:01:00Z'),('parent_share_id',child['share_id'])]:
            mutated=copy.deepcopy(child);mutated[field]=bad
            with self.subTest(field=field),self.assertRaises(ValueError):validate_chain([parent,mutated],anchors,issuer,node)
        with self.assertRaises(ValueError):validate_chain([child],anchors,issuer,node)
        with self.assertRaises(ValueError):validate_chain([parent,parent],anchors,issuer,node)
        with self.assertRaises(ValueError):validate_chain([parent],{},issuer,node)

    def test_policy_preissue_and_time_boundary_are_explicit(self):
        p=self.vectors['policy_transition'];funds={v for k,v in p.items() if k.startswith('future_')}
        validate_policy(p,funds)
        with self.assertRaises(ValueError):validate_policy(p,set())
        for key,bad in [('preissue_seconds',86401),('guard_seconds',301),('effective_at','invalid')]:
            q=copy.deepcopy(p);q[key]=bad
            with self.subTest(key=key),self.assertRaises(ValueError):validate_policy(q,funds)
        q=copy.deepcopy(p);q['new_snapshot']['snapshot_id']=q['old_snapshot']['snapshot_id']
        with self.assertRaises(ValueError):validate_policy(q,funds)
        # Oracle for the specified conservative boundary, not production implementation.
        t=timestamp(p['effective_at']);delta=dt.timedelta(seconds=1)
        choose=lambda lo,hi:'new' if lo>=t else ('deny' if hi>=t else 'old')
        self.assertEqual(choose(t-delta,t-delta),'old')
        self.assertEqual(choose(t-delta,t+delta),'deny')
        self.assertEqual(choose(t,t+delta),'new')

    def test_auth_legacy_branch_retained_and_new_fields_required(self):
        schema=load('authorization.schema.json')
        legacy=json.loads(baseline_bytes('contracts/authorization/v1/payload.schema.json'));legacy.pop('$id');legacy.pop('$schema')
        self.assertEqual(schema['oneOf'][0],legacy)
        old={'schema_version':'1.1.0','update_id':self.vectors['renewal_request']['request_id'],'node_id':self.vectors['renewal_request']['node_id'],'boot_id':self.vectors['renewal_request']['boot_id'],'signer_epoch':1,'path_generation':1,'fencing_generation_floor':1,'issued_at':'2026-11-06T00:00:00Z','backend_heartbeat_at':'2026-11-06T00:00:00Z','autonomy_deadline':'2026-11-06T01:00:00Z','authorizations':[],'revocations':[],'restrictions':{k:v['const'] for k,v in schema['oneOf'][0]['properties']['restrictions']['properties'].items()}}
        validate(old,schema)
        new=copy.deepcopy(old);new.update(schema_version='1.2.0',share_references=[self.vectors['share_reference']],policy_transitions=[self.vectors['policy_transition']]);validate(new,schema)
        template={'authorization_id':'550e8400-e29b-41d4-a716-000000000040','parent_authorization_id':self.vectors['renewal_request']['delegation_id'],'delegation_id':self.vectors['renewal_request']['delegation_id'],'share_reference_id':self.vectors['share_reference']['share_id'],'source_id':self.vectors['renewal_request']['source_id'],'kind':'credit_grant','rule_id':self.vectors['renewal_request']['rule_id'],'rule_revision':1,'path_generation':1,'issued_at':'2026-11-06T00:00:00Z','not_before':'2026-11-06T00:00:00Z','expires_at':'2026-11-06T00:05:00Z','hard_deadline':'2026-11-06T01:00:00Z','billing_snapshot_id':self.vectors['renewal_request']['billing_snapshot_id'],'amount_bytes':1024}
        new['authorizations']=[template];validate(new,schema)
        for missing in ('share_reference_id','delegation_id','billing_snapshot_id','amount_bytes'):
            bad=copy.deepcopy(new);del bad['authorizations'][0][missing]
            with self.subTest(missing=missing),self.assertRaises(ValueError):validate(bad,schema)
        for kind in ('runtime_lease','share_lease'):
            record=copy.deepcopy(template);record['kind']=kind;record.pop('amount_bytes');record.pop('billing_snapshot_id')
            if kind=='share_lease':record.update(rate_bps=1000000,connections=4)
            value=copy.deepcopy(new);value['authorizations']=[record];validate(value,schema)
        for field in ('share_references','policy_transitions'):
            bad=copy.deepcopy(new);del bad[field]
            with self.subTest(field=field),self.assertRaises(ValueError):validate(bad,schema)
        bad=copy.deepcopy(new);bad['renewal_request_id']=self.vectors['renewal_request']['request_id']
        with self.assertRaises(ValueError):validate(bad,schema)
        validate(old,schema['oneOf'][0])
        with self.assertRaises(ValueError):validate(new,schema['oneOf'][0])
        bad=copy.deepcopy(old);bad['restrictions']['existing_rules_only']=1
        with self.assertRaises(ValueError):validate(bad,schema)

    def test_runtime_legacy_input_and_new_bounded_targets(self):
        schema=load('runtime.schema.json');old={'version':1,'node_id':self.vectors['renewal_request']['node_id'],'listeners':[],'rules':[],'revocations':[]}
        validate(old,schema)
        value=copy.deepcopy(old);value['lifecycle']={'region':{'region_id':'region-a','hubs':[{'hub_id':self.vectors['hub_status']['hub_id'],'endpoint':'localhost:8443','server_name':'hub.test'}]},'probe_workers':16};validate(value,schema)
        value['lifecycle']['probe_workers']=17
        with self.assertRaises(ValueError):validate(value,schema)

    def test_protobuf_real_binary_roundtrip_and_request_digest(self):
        protoc=os.environ.get('PROTOC','protoc')
        def run(mode,kind,payload):
            cmd=[protoc,'-I',str(CANDIDATES),'--'+mode+'=vortex.control.v1.'+kind,'vortex.proto']
            return subprocess.run(cmd,input=payload,capture_output=True,check=True).stdout
        cases=[('NodeMessage','lifecycle_hello','hello'),('NodeMessage','node_report','node_report'),('NodeMessage','renewal_request','renewal_request'),('ControlMessage','hub_status','hub_status'),('ControlMessage','credential_revocations','credential_revocations'),('BackendMessage','heartbeat','backend_heartbeat'),('BackendMessage','ledger_ack','ledger_ack'),('BackendMessage','authority_restrictions','authority_restrictions'),('BackendMessage','epoch_handoff_request','epoch_handoff_request')]
        for kind,field,name in cases:
            body={field:self.vectors[name]}
            if kind=='NodeMessage':body.update(node_id=self.vectors['renewal_request']['node_id'],boot_id=self.vectors['renewal_request']['boot_id'],sequence=1)
            else:body.update(message_id='550e8400-e29b-41d4-a716-000000000099',sequence=1)
            with self.subTest(name=name):
                encoded=run('encode',kind,textproto(body).encode());decoded=run('decode',kind,encoded)
                self.assertIn(field.encode(),decoded);self.assertEqual(run('encode',kind,decoded),encoded)
        result=copy.deepcopy(self.vectors['renewal_result']);result.pop('authorization_update_sha256');result['authorization_update']={'generation':1,'payload':'{}'}
        allocation={'allocation_id':self.vectors['backend_allocation']['allocation_id'],'payload':json.dumps(self.vectors['backend_allocation'],sort_keys=True,separators=(',',':'))}
        allocation['payload_sha256']=hashlib.sha256(allocation['payload'].encode()).hexdigest()
        extra=[('ControlMessage',{'renewal_result':result}),('BackendMessage',{'allocation':allocation}),('HubMessage',{'hub_id':self.vectors['hub_status']['hub_id'],'boot_id':self.vectors['renewal_request']['boot_id'],'sequence':1,'lifecycle_hello':self.vectors['hello']}),('HubMessage',{'hub_id':self.vectors['hub_status']['hub_id'],'boot_id':self.vectors['renewal_request']['boot_id'],'sequence':2,'ledger_record':{'record_id':self.vectors['ledger_ack']['record_id'],'ledger_sequence':1,'kind':'handoff','payload':'{}','payload_sha256':hashlib.sha256(b'{}').hexdigest()}})]
        for kind,body in extra:
            with self.subTest(kind=kind,body=list(body)):
                encoded=run('encode',kind,textproto(body).encode());decoded=run('decode',kind,encoded)
                self.assertEqual(run('encode',kind,decoded),encoded)
        value=copy.deepcopy(self.vectors['renewal_request']);value.pop('request_sha256')
        encoded=run('encode','RenewalRequest',textproto(value).encode());digest=hashlib.sha256(b'vortex.renewal.v1\0'+encoded).digest()
        self.assertEqual(hashlib.sha256(b'vortex.renewal.v1\0'+run('encode','RenewalRequest',textproto(value).encode())).digest(),digest)
        value['requested_raw_bytes']+=1
        self.assertNotEqual(hashlib.sha256(b'vortex.renewal.v1\0'+run('encode','RenewalRequest',textproto(value).encode())).digest(),digest)
        with self.assertRaises(subprocess.CalledProcessError):run('encode','RenewalRequest',b'unknown_authority: 1')

    def test_existing_proto_field_types_and_tags_are_unchanged(self):
        def messages(text):
            result={}
            for match in re.finditer(r'message (\w+)\s*\{',text):
                start=match.end();depth=1;end=start
                while depth:
                    if text[end]=='{':depth+=1
                    elif text[end]=='}':depth-=1
                    end+=1
                fields=re.findall(r'(?:^|[;{])\s*(repeated\s+)?([\w.]+)\s+(\w+)\s*=\s*(\d+)',text[start:end-1],re.M)
                result[match[1]]={name:((repeated or '').strip(),kind,int(tag)) for repeated,kind,name,tag in fields}
            return result
        before=messages(baseline_bytes('contracts/protobuf/v1/vortex.proto').decode())
        after=messages((CANDIDATES/'vortex.proto').read_text())
        for message,fields in before.items():
            with self.subTest(message=message):
                self.assertIn(message,after)
                for name,value in fields.items():self.assertEqual(after[message][name],value)
                tags=[v[2] for v in after[message].values()];self.assertEqual(len(tags),len(set(tags)))

    def test_backend_restriction_and_handoff_boundaries(self):
        p=copy.deepcopy(self.vectors['epoch_handoff_request'])
        validate(p,self.schema['$defs']['epoch_handoff_request'],self.schema)
        for field,value in [('from_epoch',0),('to_epoch',MAX+1),('required_restriction_revision',0)]:
            q=copy.deepcopy(p);q[field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):validate(q,self.schema['$defs']['epoch_handoff_request'],self.schema)
        # Cross-record oracle: no transition before required restrictions or from a stale epoch.
        eligible=lambda q,epoch,revision:q['from_epoch']==epoch and q['to_epoch']==epoch+1 and q['required_restriction_revision']<=revision
        self.assertTrue(eligible(p,1,1));self.assertFalse(eligible(p,1,0));self.assertFalse(eligible(p,2,1))
        r=copy.deepcopy(self.vectors['delegation_restriction']);r.update(max_total_issued_raw_bytes=0,max_connections=0,max_rate_bps=0)
        validate(r,self.schema['$defs']['delegation_restriction'],self.schema)
        r['max_rate_bps']=10**12+1
        with self.assertRaises(ValueError):validate(r,self.schema['$defs']['delegation_restriction'],self.schema)

    def test_identity_profile_preserves_canonical_node_binding(self):
        p=load('identity-profile.json')
        self.assertEqual(set(p['extended_key_usage']),{'clientAuth','serverAuth'})
        self.assertIn('urn:vortex:node:',p['node_uri_san'])
        self.assertEqual(p['tls_version'],'1.3 only')
        self.assertIn('disabled',p['session_resumption'])
        self.assertIn('DNS-only legacy cert remains control-only',p['compatibility'])


if __name__=='__main__':
    unittest.main()
