#!/bin/sh
# Exercise explicit pool/mux over the complete authenticated runtime route.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
backend=${BACKEND:-native}
base=${MODE_EVIDENCE_DIR:-$root/artifacts/phase4/modes/$backend}
for mode in pool mux auto; do
  export TRANSPORT_MODE="$mode" BACKEND="$backend"
  export QUALIFICATION_DIR="$base/$mode/runtime" COMPARISON_DIR="$base/$mode/comparison"
  "$root/tools/phase4/qualification.sh"
  "$root/tools/phase4/comparison.sh"
done
python3 - "$base" <<'PY'
import collections, hashlib, json, re, sys
from pathlib import Path
base = Path(sys.argv[1])
result = {}
for mode in ('pool', 'mux', 'auto'):
    out = base / mode / 'comparison'
    cfg = json.loads((out/'config/source.json').read_text())
    report = json.loads((out/'comparison.json').read_text())
    if cfg['transport_mode'] != mode:
        raise SystemExit('wrong runtime transport mode')
    log = (out/'runtime-logs/source.log').read_text()
    flows = re.findall(r'flow_key=([a-f0-9]{32})/([0-9]+)/1', log)
    counts = collections.Counter(link for link, stream in flows)
    if len(flows) < 50 or not counts or max(counts.values()) < 2:
        raise SystemExit('missing evidence of authenticated link reuse across latency streams')
    pools = {}
    for role in ('source', 'relay-1', 'relay-2', 'relay-3'):
        text = (out/('runtime-logs/'+role+'.log')).read_text()
        sizes = [int(v) for v in re.findall(r'vlink outgoing lease.*pool_total=(\d+)', text)]
        if not sizes or max(sizes) > cfg['pool_max_links']:
            raise SystemExit('missing or out-of-bounds pool evidence: '+role)
        if re.search(r'vlink outgoing lease.*mode=pool network=2', text):
            raise SystemExit('UDP used exclusive TCP pool')
        pools[role] = max(sizes)
    samples = json.loads((out/'runtime-latency.json').read_text())
    workload_log = (base/mode/'runtime/logs/source.log').read_text()
    tcp_sizes = [int(v) for v in re.findall(r'vlink outgoing lease.*network=1 pool_total=(\d+)', workload_log)]
    if not tcp_sizes or max(tcp_sizes) > 4 or (mode == 'mux' and max(tcp_sizes) != 1):
        raise SystemExit('unexpected concurrent TCP pool size for '+mode)
    if mode == 'pool' and max(tcp_sizes) < 2:
        raise SystemExit('pool test did not exercise concurrent exclusive leases')
    role_modes = {}
    for role in ('source', 'relay-1', 'relay-2', 'relay-3'):
        role_log = (base/mode/'runtime/logs'/(role+'.log')).read_text()
        selected = set(re.findall(r'vlink outgoing lease.*mode=(pool|mux) network=1', role_log))
        if mode == 'auto' and selected != {'pool', 'mux'}:
            raise SystemExit('auto mode did not observe both modes on '+role)
        if mode != 'auto' and selected != {mode}:
            raise SystemExit('explicit mode selected an unexpected transport on '+role)
        role_modes[role] = sorted(selected)
    result[mode] = {'commit': report['commit'], 'dirty':report['dirty'],
        'median_ms':samples['median_ms'], 'samples_ms':samples['samples_ms'],
        'segment_median_sum_ms':report['latency']['segment_median_sum_ms'],
        'diagnostic_difference_ms':report['latency']['runtime_minus_segment_sum_ms'],
        'link_reuse_streams':dict(counts), 'observed_max_pool_links':pools,
        'workload_tcp_pool_max':max(tcp_sizes),
        'observed_tcp_modes':role_modes,
        'faults':'finite integrity/forged OPEN/bypass/middle-hop failure passed',
        'scope':'explicit bounded runtime mode, actual TCP connect to first-byte; not NFR qualification'}
if len({x['commit'] for x in result.values()}) != 1 or any(x['dirty'] for x in result.values()):
    raise SystemExit('mode SHA mismatch')
(base/'summary.json').write_text(json.dumps(result,indent=2,sort_keys=True)+'\n')
with (base/'checksums.sha256').open('w') as output:
    for p in sorted(base.rglob('*.json')):
        if 'config' not in p.parts:
            output.write(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(base))+'\n')
print(json.dumps(result,sort_keys=True))
PY
