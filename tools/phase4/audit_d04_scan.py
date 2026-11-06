#!/usr/bin/env python3
"""Recompute full-factorial D-04 evidence from raw trials before parameter review."""
import argparse
import hashlib
import json
from pathlib import Path

from audit_d04_timeline import audit as audit_timeline
from d04_run import candidates, evaluate


def read(path):
    return json.loads(path.read_text())


def audit(base):
    summary = read(base / 'summary.json')
    if summary.get('factor_policy') != 'full-factorial' or summary.get('repeats') != 3:
        raise ValueError('27 candidates with three repeats required')
    expected = {(case['id'], repeat): case
                for case in candidates(summary['reference_rate_mbps'], True)
                for repeat in range(1, 4)}
    results = summary['results']
    if len(results) != len(expected):
        raise ValueError('incomplete matrix')
    seen, commits, reviewed = set(), set(), []
    for result in results:
        case, repeat = result['case'], result['repeat']
        key = (case['id'], repeat)
        if key in seen or expected.get(key) != case:
            raise ValueError('duplicate or unexpected trial')
        seen.add(key)
        out = base / case['id'] / f'trial-{repeat}'
        recomputed = dict(case=case, repeat=repeat, **evaluate(out))
        if result != recomputed or read(out / 'd04-result.json') != recomputed:
            raise ValueError(f'summary differs from trial: {key}')
        config = read(out / 'config/source.json')['source_credit']
        for field, value in (('initial_block_bytes', case['block_bytes']),
                             ('target_seconds', case['grant_target_seconds']),
                             ('grant_target_bytes', case['grant_target_bytes']),
                             ('renew_percent', case['waterline_percent'])):
            if config.get(field) != value:
                raise ValueError(f'configured factor mismatch: {key} {field}')
        host, manifest = read(out / 'host.json'), read(out / 'semantic-manifest.json')
        configs = {p.stem: read(p) for p in sorted((out / 'config').glob('*.json'))}
        exact_digest = hashlib.sha256(json.dumps(configs, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        if exact_digest != host['exact_generated_config_sha256']:
            raise ValueError('generated config digest mismatch')
        if host.get('backend') != 'native' or host.get('system') != 'Linux' or host.get('os_id') != 'ubuntu' or host.get('os_version') != '24.04':
            raise ValueError('native Ubuntu matrix evidence required')
        digest = hashlib.sha256(json.dumps(manifest, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        if digest != host['semantic_manifest_sha256']:
            raise ValueError('semantic manifest digest mismatch')
        workload = manifest['workload']
        if workload['tcp_streams'] != 16 or workload['short_flow_samples'] != 50 or workload['udp_mbps'] != 100 or workload['duration_seconds'] < 15:
            raise ValueError('required mixed/short-flow workload missing')
        window = read(out / 'idle-window.json')
        if window.get('requested_seconds', 0) < 65 or window.get('elapsed_nanoseconds', 0) < 65_000_000_000:
            raise ValueError('post-workload idle window missing')
        timeline = audit_timeline(read(out / 'events-source.jsonl.credit-timeline.json'), case['grant_target_bytes'], True)
        commits.add(host['commit'])
        reviewed.append(dict(case=case['id'], repeat=repeat, timeline=timeline,
                             source_settled=True, raw_result_sha256=hashlib.sha256((out/'d04-result.json').read_bytes()).hexdigest()))
    if len(commits) != 1:
        raise ValueError('mixed source commits')
    return dict(status='pass', scope='full-factorial raw trial and sampled timeline reconciliation',
                trials=len(reviewed), commit=next(iter(commits)), locked=False,
                production_durable_issuer_qualified=False, trials_audited=reviewed)


def main():
    parser = argparse.ArgumentParser(__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    result = audit(args.directory)
    encoded = json.dumps(result, indent=2, sort_keys=True) + '\n'
    if args.output:
        args.output.write_text(encoded)
    print(json.dumps({key: value for key, value in result.items() if key != 'trials_audited'}, sort_keys=True))


if __name__ == '__main__':
    main()
