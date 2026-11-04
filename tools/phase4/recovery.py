#!/usr/bin/env python3
"""Real tracer process drain/switch/restart regression, native or Compose."""
import argparse, copy, hashlib, json, os, signal, socket, subprocess, time
from pathlib import Path

ap=argparse.ArgumentParser();ap.add_argument('--backend',choices=['native','docker'],default=os.getenv('BACKEND','native'));ap.add_argument('--out');args=ap.parse_args()
root=Path(__file__).resolve().parents[2];out=Path(args.out or root/'artifacts/phase4/recovery'/args.backend).resolve();out.mkdir(parents=True,exist_ok=True)
env=dict(os.environ,QUALIFICATION_DIR=str(out),TRANSPORT_MODE='mux')
procs={};handles=[];backend=args.backend
roles=['source','relay-1','relay-2','relay-3','destination','relay-2b']
def run(cmd,**kw):return subprocess.run(cmd,check=True,env=env,**kw)
def waitfor(fn,timeout=30):
 deadline=time.monotonic()+timeout
 while time.monotonic()<deadline:
  if fn():return
  time.sleep(.1)
 raise RuntimeError('condition timed out')
def write(path,value):
 temp=path.with_suffix('.tmp');temp.write_text(json.dumps(value));temp.replace(path)
def compose(*cmd):return ['docker','compose','-f',str(root/'test/qualification/compose.yaml'),'-f',str(out/'compose-extra.json'),*cmd]
def start(role):
 if backend=='docker':run(compose('up','-d',role),stdout=subprocess.DEVNULL)
 else:
  log=(out/'logs'/f'{role}.log').open('ab');handles.append(log)
  procs[role]=subprocess.Popen([str(root/'artifacts/phase4/vortex-relay'),'--config',str(out/'config'/f'{role}.json')],stdout=log,stderr=log,env=env)
def logs(role):
 if backend=='docker':return subprocess.check_output(compose('logs','--no-color',role),env=env,text=True)
 return (out/'logs'/f'{role}.log').read_text()
def notify(role):
 if backend=='docker':run(compose('kill','-s','SIGHUP',role),stdout=subprocess.DEVNULL)
 else:os.kill(procs[role].pid,signal.SIGHUP)
def stop(role):
 if backend=='docker':run(compose('stop',role),stdout=subprocess.DEVNULL)
 else:procs[role].terminate();procs[role].wait(timeout=10)
def echo(conn,data):
 conn.sendall(data);received=b''
 while len(received)<len(data):
  chunk=conn.recv(len(data)-len(received))
  if not chunk:raise RuntimeError('truncated TCP echo')
  received+=chunk
 if received!=data:raise RuntimeError('TCP echo mismatch')
def fresh(data):
 with socket.create_connection(('127.0.0.1',18080),timeout=3) as s:echo(s,data)
def probe():
 try:fresh(b'readiness');return True
 except OSError:return False

def udp_echo(s,data):
 s.sendto(data,('127.0.0.1',18082))
 if s.recv(1024)!=data:raise RuntimeError('UDP replay/mismatch')
report={'scope':'finite tracer hot-reload/drain/restart; no durable activation or authoritative ledger recovery','cases':{}}
try:
 run([str(root/'tools/phase4/generate-qualification.sh'),backend,str(out)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 cfg={r:json.loads((out/'config'/f'{r}.json').read_text()) for r in roles[:-1]}
 # The spare has its own authenticated node identity and preserves all hops.
 spare_id='00000000-0000-0000-0000-000000000006'
 certs=out/'certs';key=certs/'relay-2b-key.pem';cert=certs/'relay-2b.pem'
 run(['openssl','genpkey','-algorithm','ED25519','-out',str(key)],stderr=subprocess.DEVNULL)
 run(['openssl','req','-new','-key',str(key),'-out',str(certs/'spare.csr'),'-subj','/CN=relay-2b'],stderr=subprocess.DEVNULL)
 (certs/'spare.ext').write_text('subjectAltName=DNS:vortex,URI:urn:vortex:node:'+spare_id+'\nextendedKeyUsage=clientAuth,serverAuth\n')
 run(['openssl','x509','-req','-in',str(certs/'spare.csr'),'-CA',str(certs/'ca.pem'),'-CAkey',str(certs/'ca-key.pem'),'-CAcreateserial','-out',str(cert),'-days','2','-extfile',str(certs/'spare.ext')],stderr=subprocess.DEVNULL)
 cfg['relay-2b']=copy.deepcopy(cfg['relay-2']);c=cfg['relay-2b'];c['node_id']=spare_id;c['listen']='127.0.0.1:19446' if backend=='native' else '0.0.0.0:9446';c['certificate']=str(cert) if backend=='native' else '/certs/relay-2b.pem';c['private_key']=str(key) if backend=='native' else '/certs/relay-2b-key.pem';c['evidence_file']=str(out/'events-relay-2b.jsonl')
 startup_hashes={}
 for r,c in cfg.items():
  segment={k:c.get(k,'') for k in ('peer','peer_node_id','next_hop_node_id','hop_index')}
  alt=copy.deepcopy(segment)
  if r=='relay-1':alt['peer']='127.0.0.1:19446' if backend=='native' else 'relay-2b:9446';alt['next_hop_node_id']=spare_id
  if r=='relay-3':alt['peer_node_id']=spare_id
  c['routes']={'primary':segment,'alternate':alt};c['active_route']='primary'
  write(out/'config'/f'{r}.json',c)
  startup_hashes[r]=hashlib.sha256((out/'config'/f'{r}.json').read_bytes()).hexdigest()
 report['startup_config_sha256']=startup_hashes
 if backend=='docker':
  import_config={'services':{'relay-2b':{'extends':{'file':str(root/'test/qualification/compose.yaml'),'service':'relay-2'},'command':['--config','/config/relay-2b.json']}}}
  write(out/'compose-extra.json',import_config)
  run(compose('up','--build','-d'),stdout=subprocess.DEVNULL)
 else:
  (out/'logs').mkdir(exist_ok=True);(root/'artifacts/phase4').mkdir(exist_ok=True)
  run(['go','build','-o',str(root/'artifacts/phase4/vortex-relay'),'./cmd/vortex-relay'],cwd=root/'engine')
  targetlog=(out/'logs/target.log').open('ab');handles.append(targetlog)
  procs['target']=subprocess.Popen(['python3',str(root/'tools/phase4/echo_target.py'),'--host','127.0.0.1'],stdout=targetlog,stderr=targetlog)
  for r in roles:start(r)
 waitfor(probe)
 write(out/'config/old-source.json',cfg['source'])
 old=socket.create_connection(('127.0.0.1',18080),timeout=3);echo(old,b'old-generation-before-switch')
 drain=socket.create_connection(('127.0.0.1',18080),timeout=3);echo(drain,b'old-to-drain')
 udp=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);udp.settimeout(3);udp_echo(udp,b'udp-generation-1')
 # Unlisted route and stale generation reloads must leave the live path intact.
 bad=copy.deepcopy(cfg['source']);bad['active_route']='unlisted';bad['path_generation']=2;write(out/'config/source.json',bad);notify('source')
 waitfor(lambda:'runtime route reload rejected' in logs('source'));fresh(b'after-rejected-unlisted')
 bad=copy.deepcopy(cfg['source']);write(out/'config/source.json',bad);before=logs('source').count('runtime route reload rejected');notify('source');waitfor(lambda:logs('source').count('runtime route reload rejected')>before)
 report['cases']['unlisted_and_stale_reload']='rejected; existing route preserved'
 stop('relay-2b')
 unavailable=copy.deepcopy(cfg['relay-1']);unavailable['active_route']='alternate';unavailable['path_generation']=2
 write(out/'config/relay-1.json',unavailable);notify('relay-1')
 waitfor(lambda:'runtime route reload rejected' in logs('relay-1'));fresh(b'failed-prewarm-kept-primary')
 ready_count=logs('relay-2b').count('vortex-relay ready');start('relay-2b');waitfor(lambda:logs('relay-2b').count('vortex-relay ready')>ready_count)
 report['cases']['unhealthy_candidate']='prewarm failed; active primary preserved'

 # Activate downstream first; the old established TCP remains on its old transports.
 for r in ['destination','relay-3','relay-2b','relay-1','source']:
  cfg[r]['active_route']='alternate';cfg[r]['path_generation']=2;write(out/'config'/f'{r}.json',cfg[r]);notify(r)
  waitfor(lambda r=r:'runtime route activated route=alternate generation=2' in logs(r))
 echo(old,b'old-generation-still-drains')
 echo(drain,b'drain-response-after-switch');drain.shutdown(socket.SHUT_WR)
 if drain.recv(100):raise RuntimeError('unexpected trailing bytes during old TCP drain')
 drain.close();report['cases']['old_tcp_halfclose']='old-generation response and graceful EOF preserved'
 waitfor(lambda:'udp source session closed generation=1' in logs('source'))

 fresh(b'new-generation-authorized-alternate');time.sleep(.2);udp_echo(udp,b'udp-generation-2')
 # Use the actual old authenticated source to attempt a fresh old OPEN.
 if backend=='native':
  run(['go','run','./cmd/phase4-probe','--config',str(out/'config/old-source.json'),'--stale-only','--output',str(out/'stale-open.json')],cwd=root/'engine')
 else:run(compose('exec','-T','source','/usr/local/bin/phase4-probe','--config','/config/old-source.json','--stale-only','--output',str(out/'stale-open.json')))
 report['cases']['old_generation_open']='rejected with stale revision code 7'
 waitfor(lambda:'udp source session opened generation=2' in logs('source'))
 report['cases']['generation_switch']='generation 2 on authorized spare; old TCP continues; UDP reopens'
 # Kill old middle relay: only its old TCP dies; new flows stay on spare.
 stop('relay-2');old.settimeout(3)
 try:
  old.sendall(b'old-path-after-fault')
  if old.recv(100):raise RuntimeError('old TCP migrated or bypassed dead hop')
 except (BrokenPipeError,ConnectionResetError):pass
 old.close();waitfor(lambda:'runtime route drained generation=1' in logs('source'));fresh(b'alternate-survives-primary-fault');udp_echo(udp,b'alternate-udp-survives')
 report['cases']['primary_fault']='old TCP terminated; alternate TCP/UDP unaffected'
 # Kill active middle relay, ensure no silent fallback or bypass.
 active=socket.create_connection(('127.0.0.1',18080),timeout=3);echo(active,b'before-active-fault');stop('relay-2b')
 try:
  active.sendall(b'after-active-fault')
  if active.recv(100):raise RuntimeError('active stream bypassed failed spare')
 except (BrokenPipeError,ConnectionResetError):pass
 active.close()
 try:fresh(b'must-not-fallback');raise RuntimeError('new TCP bypassed failed active path')
 except (OSError,RuntimeError) as e:
  if str(e)=='new TCP bypassed failed active path':raise
 report['cases']['active_fault']='established TCP terminated; no fallback without activation'
 # Restart same authorized endpoint; reconnect new streams, never replay old payload.
 start('relay-2b');waitfor(probe);fresh(b'new-after-restart');time.sleep(.2);udp_echo(udp,b'new-udp-after-restart');udp.close()
 report['cases']['restart']='fresh TCP/UDP recovered on authorized generation 2; old TCP not replayed'
 report['status']='pass'
finally:
 for r in roles:
  try:(out/'logs').mkdir(exist_ok=True);(out/'logs'/f'{r}.log').write_text(logs(r)) if backend=='docker' else None
  except Exception:pass
 if backend=='docker':
  if (out/'compose-extra.json').exists():subprocess.run(compose('down','--remove-orphans'),env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 else:
  for p in procs.values():
   if p.poll() is None:p.terminate()
  for p in procs.values():
   try:p.wait(timeout=15)
   except subprocess.TimeoutExpired:p.kill();p.wait()
 for h in handles:h.close()
 report['commit']=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip();report['dirty']=bool(subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True));report['backend']=backend
 write(out/'recovery.json',report)
 (out/'checksums.sha256').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(out))+'\n' for p in sorted(out.rglob('*')) if p.is_file() and (p.suffix=='.log' or p.name in ('recovery.json','stale-open.json'))))
print(json.dumps(report,sort_keys=True))
