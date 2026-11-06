#!/usr/bin/env python3
"""Independent inspection of real process policy snapshots; never authority."""
import gzip,hashlib,json,pathlib,sqlite3,sys,tempfile
out=pathlib.Path(sys.argv[1]);cases=[]
for label in ('policy-cutover','policy-future-revoked','policy-hub-restart'):
    meta=json.loads((out/f'{label}-ledgers.json').read_text());hashes=meta['hashes']
    for role in ('agent','hub'):
        raw=gzip.decompress((out/f'{label}-{role}.sqlite.gz').read_bytes())
        assert hashlib.sha256(raw).hexdigest()==hashes[role]
        with tempfile.TemporaryDirectory() as tmp:
            path=pathlib.Path(tmp)/'snapshot.db';path.write_bytes(raw)
            db=sqlite3.connect(f'file:{path}?mode=ro',uri=True)
            assert db.execute('pragma integrity_check').fetchone()[0]=='ok'
            assert meta['source_journal_modes'][role]=='wal'
            assert db.execute('pragma journal_mode').fetchone()[0] in ('wal','delete')
            if role=='agent':
                rows=db.execute('select payload,payload_sha256 from policy_snapshots').fetchall()
                assert len(rows)==2
                for payload,digest in rows:assert hashlib.sha256(payload).digest()==digest
                bindings=[json.loads(row[0]) for row in rows]
                assert sorted(x['coefficient_milli'] for x in bindings)==[1000,1500]
                for binding in bindings:
                    count=db.execute('select count(*) from runtime_sessions s join authorization_records a on a.node_id=s.node_id and a.authorization_id=s.initial_grant_id where a.billing_snapshot_id=?',(binding['snapshot_id'],)).fetchone()[0]
                    assert count>=2,(label,binding,count)
                assert db.execute("select count(*) from config_share_holds where state='held'").fetchone()[0]>=3
                assert db.execute("select unknown_upper_bound,liability_state from unknown_usage_batches where batch_id='command-debt'").fetchone()==('512','unresolved')
                for payload,digest in db.execute('select payload,payload_sha256 from policy_transitions'):assert hashlib.sha256(payload).digest()==digest
                if label=='policy-future-revoked':assert db.execute('select count(*) from authorization_revocations r join policy_transitions p on r.authorization_id=p.transition_id').fetchone()[0]==1
            else:
                # Separate funded old/new source debits, no repeated allocation.
                rows=db.execute('select remaining_bytes,payload from issuer_delegations').fetchall()
                assert len(rows)==2
                sources=[]
                for remaining,payload in rows:
                    d=json.loads(payload);assert int(d['RawBytes'])-int(remaining)==8192
                    sources.append(d['SourceID'])
                assert len(set(sources))==2
                assert db.execute('select count(*) from issuer_issues').fetchone()[0]==2
            db.close()
    cases.append(label)
print(json.dumps({'schema':1,'status':'passed','scope':'finite-share-policy-process','snapshots':len(cases)*2,'cases':cases,'production_backend_authority':False}))
