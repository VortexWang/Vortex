#!/usr/bin/env python3
"""Static-snapshot TCP source credit over actual three-relay processes."""
import argparse, hashlib, json, os, socket, subprocess, time, urllib.request
from pathlib import Path

ap = argparse.ArgumentParser()
ap.add_argument('--backend', choices=['native', 'docker'], default=os.getenv('BACKEND', 'native'))
ap.add_argument('--out')
ap.add_argument('--protocol', choices=['tcp','udp'], default='tcp')
args = ap.parse_args()
root = Path(__file__).resolve().parents[2]
out = Path(args.out or root/'artifacts/phase4/credit'/(args.backend+('-udp' if args.protocol=='udp' else ''))).resolve()
out.mkdir(parents=True, exist_ok=True)
env = dict(os.environ, QUALIFICATION_DIR=str(out), TRANSPORT_MODE='mux')
roles = ['source', 'relay-1', 'relay-2', 'relay-3', 'destination']
procs, handles = {}, []

def run(cmd, **kw):
    return subprocess.run(cmd, check=True, env=env, **kw)

def compose(*cmd):
    return ['docker', 'compose', '-f', str(root/'test/qualification/compose.yaml'), *cmd]

def waitfor(fn, timeout=90):
    deadline = time.monotonic()+timeout
    while time.monotonic() < deadline:
        try:
            if fn(): return
        except (OSError, ValueError): pass
        time.sleep(.1)
    raise RuntimeError('credit topology readiness timed out')

def log(role):
    if args.backend == 'docker':
        return subprocess.check_output(compose('logs', '--no-color', role), env=env, text=True)
    return (out/'logs'/f'{role}.log').read_text()

def stats():
    with urllib.request.urlopen('http://127.0.0.1:18083/stats', timeout=3) as r:
        return json.load(r)

def stop():
    if args.backend == 'docker':
        run(compose('stop'), stdout=subprocess.DEVNULL)
        for role in roles:
            (out/'logs'/f'{role}.log').write_text(log(role))
            cid = subprocess.check_output(compose('ps', '-a', '-q', role), env=env, text=True).strip()
            code = subprocess.check_output(['docker','inspect','--format','{{.State.ExitCode}}',cid], text=True).strip()
            if code != '0': raise RuntimeError(f'{role} exit {code}')
    else:
        # Source first: flush its accounting after all accepted workers settle.
        for role in roles:
            p=procs[role]; p.terminate()
            if p.wait(timeout=15) != 0: raise RuntimeError(f'{role} failed')

report = {'status':'fail', 'scope':'finite static-snapshot source credit; no durable issuer, switching or D-04 approval', 'protocol':args.protocol,
          'commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),
          'dirty':bool(subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip()),
          'backend':args.backend, 'cases':{}}
try:
    run([str(root/'tools/phase4/generate-qualification.sh'),args.backend,str(out)], stdout=subprocess.DEVNULL)
    cfgpath = out/'config/source.json'
    cfg = json.loads(cfgpath.read_text())
    if args.protocol=='tcp': cfg.pop('udp_service_listen', None)
    cfg['execution_point'] = True
    cfg['source_credit'] = dict(snapshot_id='qualification-rule/source/path-1/policy-1', budget_bytes=262144,
                               initial_block_bytes=16384, max_block_bytes=65536, grant_target_bytes=65536, renew_percent=30)
    cfgpath.write_text(json.dumps(cfg))
    run(['python3', str(root/'tools/phase4/record_evidence.py'), str(out), args.backend])
    manifest_path=out/'semantic-manifest.json'
    manifest=json.loads(manifest_path.read_text())
    manifest['workload']={'protocol':args.protocol,'exchanges':8 if args.protocol=='tcp' else 64,'payload_bytes':16384 if args.protocol=='tcp' else 1024,'budget_bytes':262144,'exhausted_existing_and_new':True,'interleaved_tcp_exchanges':4 if args.protocol=='udp' else 0}
    manifest_path.write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n')
    host_path=out/'host.json';host=json.loads(host_path.read_text())
    host['driver_placement']='host-python-credit-probe'
    host['semantic_manifest_sha256']=hashlib.sha256(json.dumps(manifest,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    host_path.write_text(json.dumps(host,indent=2,sort_keys=True)+'\n')
    report['semantic_manifest_sha256']=host['semantic_manifest_sha256']
    report['source_config_sha256'] = hashlib.sha256(cfgpath.read_bytes()).hexdigest()
    evidence = Path(cfg['evidence_file']+'.credit.json')
    evidence.write_text(''); evidence.chmod(0o666)  # host-owned artifact, even with a root container
    (out/'logs').mkdir(exist_ok=True)
    if args.backend == 'docker':
        run(compose('up','-d','--build',*roles,'target'), stdout=subprocess.DEVNULL)
    else:
        run(['go','build','-o',str(root/'artifacts/phase4/vortex-relay'),'./cmd/vortex-relay'],cwd=root/'engine')
        for role in ['target',*reversed(roles)]:
            h=(out/'logs'/f'{role}.log').open('wb');handles.append(h)
            cmd=['python3',str(root/'tools/phase4/echo_target.py'),'--host','127.0.0.1'] if role=='target' else [str(root/'artifacts/phase4/vortex-relay'),'--config',str(out/'config'/f'{role}.json')]
            procs[role]=subprocess.Popen(cmd,stdout=h,stderr=h,env=env)
    waitfor(lambda: all('vlink next-hop ready' in log(r) for r in roles[1:-1]) and 'vortex-relay ready' in log('source'))
    waitfor(lambda: bool(stats()))
    before=stats()
    # Readiness never sends application bytes or consumes the finite budget.
    if args.protocol=='tcp':
        with socket.create_connection(('127.0.0.1',18080),timeout=5) as conn:
            conn.settimeout(5)
            for i in range(8):
                payload=bytes([65+i])*16384
                conn.sendall(payload); received=b''
                while len(received)<len(payload):
                    chunk=conn.recv(len(payload)-len(received))
                    if not chunk: raise RuntimeError('authorized echo truncated')
                    received+=chunk
                if received!=payload: raise RuntimeError('echo integrity')
            report['cases']['within_budget'] = 'eight 16384-byte requests and responses accepted'
            conn.sendall(b'forbidden-after-exhaustion')
            try: reply=conn.recv(128)
            except ConnectionResetError: reply=b''
            if reply: raise RuntimeError('exhausted existing flow forwarded')
    else:
        with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as conn:
            conn.settimeout(5)
            for i in range(64):
                if i==32:
                    with socket.create_connection(('127.0.0.1',18080),timeout=5) as tcp:
                        tcp.settimeout(5)
                        for j in range(4):
                            data=bytes([200+j])*16384;tcp.sendall(data);received=b''
                            while len(received)<len(data):
                                chunk=tcp.recv(len(data)-len(received))
                                if not chunk:raise RuntimeError('interleaved TCP truncated')
                                received+=chunk
                            if received!=data:raise RuntimeError('interleaved TCP integrity')
                payload=bytes([i])*1024
                conn.sendto(payload,('127.0.0.1',18082))
                if conn.recv(2048)!=payload:raise RuntimeError('UDP echo integrity')
            report['cases']['within_budget']='64 x 1024-byte UDP and 4 x 16384-byte interleaved TCP echoes accepted'
            conn.settimeout(.4)
            conn.sendto(b'forbidden-old-udp',('127.0.0.1',18082))
            try: reply=conn.recv(128)
            except socket.timeout: reply=b''
            if reply:raise RuntimeError('exhausted UDP flow forwarded')
        with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as conn:
            conn.settimeout(.4)
            conn.sendto(b'forbidden-new-udp',('127.0.0.1',18082))
            try: reply=conn.recv(128)
            except socket.timeout: reply=b''
            if reply:raise RuntimeError('new UDP flow bypassed budget')
    with socket.create_connection(('127.0.0.1',18080),timeout=5) as conn:
        conn.settimeout(5)
        try:
            conn.sendall(b'forbidden-new-flow'); reply=conn.recv(128)
        except ConnectionResetError: reply=b''
        if reply: raise RuntimeError('exhausted new flow forwarded')
    time.sleep(.2)
    after=stats()
    # The source denial must precede data delivery to the target.
    if after['tcp_bytes_received']-before['tcp_bytes_received'] != (131072 if args.protocol=='tcp' else 65536) or after['udp_bytes_received']-before['udp_bytes_received'] != (65536 if args.protocol=='udp' else 0):
        raise RuntimeError(f'target received unauthorized bytes: {before} -> {after}')
    report['cases']['exhausted_existing_and_new'] = 'no response and no extra target bytes'
    stop()
    state=json.loads(evidence.read_text());report['credit']=state
    if state['used_bytes']!=262144 or state['held_bytes']!=0 or state['unknown_bytes']!=0 or state['sessions']!=0 or state['denials']<2 or state['grants']<2:
        raise RuntimeError(f'credit settlement incomplete: {state}')
    if sum(state[k] for k in ('unissued_bytes','pending_grant_bytes','unknown_grant_bytes','available_bytes','held_bytes','used_bytes','unknown_bytes'))!=state['budget_bytes']:
        raise RuntimeError('credit conservation violated')
    report['cases']['conservation'] = '262144 bytes used exactly once, no held/session leak'
    report['status']='pass'
except Exception as exc:
    report['error']=str(exc)
    raise
finally:
    if args.backend=='docker':
        for r in roles:
            try: (out/'logs'/f'{r}.log').write_text(log(r))
            except Exception: pass
        subprocess.run(compose('down','--remove-orphans'),env=env,stdout=subprocess.DEVNULL)
    else:
        for p in procs.values():
            if p.poll() is None:p.terminate()
        for p in procs.values():p.wait(timeout=15)
    for h in handles:h.close()
    (out/'credit-result.json').write_text(json.dumps(report,indent=2)+'\n')
    files=[out/'credit-result.json', out/'host.json', out/'semantic-manifest.json',*sorted((out/'logs').glob('*.log'))]
    if (out/'events-source.jsonl.credit.json').exists():files.append(out/'events-source.jsonl.credit.json')
    (out/'checksums.sha256').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(out))+'\n' for p in files))
print(json.dumps(report))
