#!/usr/bin/env python3
"""Exercise immutable billing snapshot selection on a static three-relay route."""
import hashlib, json, os, signal, socket, subprocess, time, urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BACKEND = os.getenv('BACKEND', 'native')
OUT = Path(os.getenv('SNAPSHOT_OUT', ROOT/'artifacts/phase4/snapshots'/BACKEND)).resolve()
OUT.mkdir(parents=True, exist_ok=True)
ENV = dict(os.environ, QUALIFICATION_DIR=str(OUT), TRANSPORT_MODE='mux')
ROLES = ['source', 'relay-1', 'relay-2', 'relay-3', 'destination']
PROCS, HANDLES = {}, []
COMPOSE = ['docker', 'compose', '-f', str(ROOT/'test/qualification/compose.yaml')]

def run(cmd, **kw): return subprocess.run(cmd, check=True, env=ENV, **kw)
def log(role):
    if BACKEND == 'docker': return subprocess.check_output(COMPOSE+['logs','--no-color',role],env=ENV,text=True)
    return (OUT/'logs'/f'{role}.log').read_text()
def waitfor(fn, timeout=90):
    end=time.monotonic()+timeout
    while time.monotonic()<end:
        try:
            if fn(): return
        except (OSError,ValueError): pass
        time.sleep(.1)
    raise RuntimeError('snapshot topology or selection timed out')
def stats():
    with urllib.request.urlopen('http://127.0.0.1:18083/stats',timeout=3) as r: return json.load(r)
def exchange(conn, payload):
    conn.sendall(payload); got=b''
    while len(got)<len(payload):
        part=conn.recv(len(payload)-len(got))
        if not part: raise RuntimeError('truncated echo')
        got+=part
    if got!=payload: raise RuntimeError('echo integrity')

report={'status':'fail','scope':'static routing; immutable billing snapshot select for new source sessions; no path switch or durable billing claim',
        'backend':BACKEND,'commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),
        'dirty':bool(subprocess.check_output(['git','status','--porcelain'],cwd=ROOT,text=True).strip()),'cases':{}}
try:
    run([str(ROOT/'tools/phase4/generate-qualification.sh'),BACKEND,str(OUT)],stdout=subprocess.DEVNULL)
    cfgpath=OUT/'config/source.json'; cfg=json.loads(cfgpath.read_text()); cfg['execution_point']=True
    def version(sid, coefficient):
        return {'binding':{'rule_id':cfg['rule_id'],'execution_node':cfg['node_id'],'path_generation':cfg['path_generation'],'policy_revision':1,'coefficient_milli':coefficient},
                'credit':{'snapshot_id':sid,'budget_bytes':131072,'initial_block_bytes':65536,'max_block_bytes':65536,'grant_target_bytes':65536,'renew_percent':30}}
    cfg.pop('source_credit',None)
    cfg['source_credit_book']={'funding_bytes':393216,'active':'old','selection_revision':1,'snapshots':{'old':version('old',1000),'new':version('new',2000)}}
    cfgpath.write_text(json.dumps(cfg))
    evidence=Path(cfg['evidence_file']+'.credit-book.json'); evidence.write_text(''); evidence.chmod(0o666)
    (OUT/'logs').mkdir(exist_ok=True)
    if BACKEND=='docker': run(COMPOSE+['up','-d','--build',*ROLES,'target'],stdout=subprocess.DEVNULL)
    else:
        run(['go','build','-o',str(ROOT/'artifacts/phase4/vortex-relay'),'./cmd/vortex-relay'],cwd=ROOT/'engine')
        for role in ['target',*reversed(ROLES)]:
            h=(OUT/'logs'/f'{role}.log').open('wb'); HANDLES.append(h)
            cmd=['python3',str(ROOT/'tools/phase4/echo_target.py'),'--host','127.0.0.1'] if role=='target' else [str(ROOT/'artifacts/phase4/vortex-relay'),'--config',str(OUT/'config'/f'{role}.json')]
            PROCS[role]=subprocess.Popen(cmd,stdout=h,stderr=h,env=ENV)
    waitfor(lambda: all('vlink next-hop ready' in log(r) for r in ROLES[1:-1]) and 'vortex-relay ready' in log('source'))
    waitfor(lambda: bool(stats()))
    before=stats(); old=socket.create_connection(('127.0.0.1',18080),timeout=5); old.settimeout(5)
    exchange(old,b'old-session-before-selection')
    cfg['source_credit_book']['active']='new'; cfg['source_credit_book']['selection_revision']=2
    cfgpath.write_text(json.dumps(cfg))
    if BACKEND=='docker': run(COMPOSE+['kill','-s','SIGHUP','source'])
    else: os.kill(PROCS['source'].pid,signal.SIGHUP)
    waitfor(lambda: 'runtime credit snapshot selected snapshot=new revision=2' in log('source'))
    exchange(old,b'old-session-after-selection')
    with socket.create_connection(('127.0.0.1',18080),timeout=5) as new:
        new.settimeout(5); exchange(new,b'new-session-after-selection')
    old.close(); time.sleep(.2); after=stats()
    if after['tcp_bytes_received']-before['tcp_bytes_received'] != len(b'old-session-before-selectionold-session-after-selectionnew-session-after-selection'):
        raise RuntimeError(f'unexpected target bytes: {before} -> {after}')
    report['cases']['session-affinity']='existing TCP session retained old snapshot; post-selection TCP session used new snapshot'
    report['cases']['route-boundary']='all traffic remained on startup-pinned static route'
    report['target_bytes']=after['tcp_bytes_received']-before['tcp_bytes_received']
    if BACKEND=='docker':
        run(COMPOSE+['stop'],stdout=subprocess.DEVNULL)
        for role in ROLES:
            (OUT/'logs'/f'{role}.log').write_text(log(role))
            cid=subprocess.check_output(COMPOSE+['ps','-a','-q',role],env=ENV,text=True).strip()
            if subprocess.check_output(['docker','inspect','--format','{{.State.ExitCode}}',cid],text=True).strip()!='0': raise RuntimeError(f'{role} did not exit cleanly')
    else:
        for role in ROLES:
            PROCS[role].terminate()
        for role in ROLES:
            if PROCS[role].wait(timeout=30)!=0: raise RuntimeError(f'{role} failed')
            (OUT/'logs'/f'{role}.log').read_text()
    state=json.loads(evidence.read_text()); oldstate=state['snapshots']['old']['credit']; newstate=state['snapshots']['new']['credit']
    expected=len(b'old-session-before-selectionold-session-after-selectionnew-session-after-selection')
    if state['active']!='new' or state['selection_revision']!=2 or oldstate['used_bytes']!=2*len(b'old-session-before-selectionold-session-after-selection') or newstate['used_bytes']!=2*len(b'new-session-after-selection') or oldstate['sessions'] or newstate['sessions']:
        raise RuntimeError(f'snapshot settlement mismatch: {state}')
    report['book_state']=state; report['cases']['billing-affinity']='old and new usage settled only in their immutable snapshot gates'
    report['semantic_manifest_sha256']=hashlib.sha256(json.dumps({'payloads':['old-session-before-selection','old-session-after-selection','new-session-after-selection'],'expected_target_bytes':expected},sort_keys=True,separators=(',',':')).encode()).hexdigest()
    report['status']='pass'
except Exception as exc:
    report['error']=str(exc); raise
finally:
    if BACKEND=='docker':
        for role in ROLES:
            try: (OUT/'logs'/f'{role}.log').write_text(log(role))
            except Exception: pass
        subprocess.run(COMPOSE+['down','--remove-orphans'],env=ENV,stdout=subprocess.DEVNULL)
    else:
        for p in PROCS.values():
            if p.poll() is None: p.terminate()
        for p in PROCS.values():
            try: p.wait(timeout=30)
            except subprocess.TimeoutExpired: p.kill(); p.wait()
        for h in HANDLES: h.close()
    (OUT/'snapshot-result.json').write_text(json.dumps(report,indent=2)+'\n')
    files=[OUT/'snapshot-result.json',*sorted((OUT/'logs').glob('*.log'))]
    if evidence.exists(): files.append(evidence)
    (OUT/'checksums.sha256').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(OUT))+'\n' for p in files))
print(json.dumps(report))
