#!/usr/bin/env python3
"""Recompute full-factorial D-04 evidence from raw trials before parameter review."""
import argparse
import hashlib
import json
import re
from pathlib import Path

from audit_d04_timeline import audit as audit_timeline
from d04_run import candidates, evaluate


def read(path):
    return json.loads(path.read_text())


def audit_accounting(out):
    flows, report = read(out / 'flow-accounting.json'), read(out / 'report.json')
    workload = read(out / 'mixed-workload.json')
    source = read(out / 'events-source.jsonl.credit.json')
    before, after = read(out / 'target-start.json'), read(out / 'target-final.json')
    delta = {}
    for key in ('tcp_bytes_received', 'tcp_bytes_sent', 'udp_bytes_received', 'udp_bytes_sent'):
        if any(type(data.get(key)) is not int or data[key] < 0 for data in (before, after)) or after[key] < before[key]:
            raise ValueError('invalid target byte counter')
        delta[key] = after[key] - before[key]
    totals = {'left_to_right': 0, 'right_to_left': 0}
    expected_counts = {'tcp': 1 + 2 * workload['tcp_streams'] + 55, 'udp': 2}
    for kind, network in (('tcp', 1), ('udp', 2)):
        if len(flows[kind]) != expected_counts[kind]:
            raise ValueError('missing mixed or short-flow accounting identities')
        for identity, directions in flows[kind].items():
            if not re.fullmatch(r'[0-9a-f]{32}/[1-9][0-9]*/' + str(network), identity):
                raise ValueError('invalid flow identity')
            if set(directions) != set(totals) or any(type(v) is not int or v < 0 for v in directions.values()):
                raise ValueError('invalid per-flow amount')
            if kind == 'tcp' and directions['left_to_right'] != directions['right_to_left']:
                raise ValueError('TCP echo did not reconcile per flow')
            for direction, amount in directions.items():
                totals[direction] += amount
    drops = report['classified_drops']
    returned = report['returned_reservations']
    expected = {'left_to_right': delta['tcp_bytes_received'] + delta['udp_bytes_received'] + 1200 * drops.get('kernel_rcvbuf_container_target', 0),
                'right_to_left': delta['tcp_bytes_sent'] + delta['udp_bytes_sent'] - returned.get('right_to_left', 0) - 1200 * drops.get('kernel_rcvbuf_container_destination', 0)}
    if totals != expected or totals != report['accounting'] or sum(totals.values()) != source['used_bytes']:
        raise ValueError('target, per-flow, report and source byte totals differ')
    return dict(flow_counts=expected_counts, bytes_by_direction=totals, source_used_bytes=source['used_bytes'])


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
        accounting = audit_accounting(out)
        timeline = audit_timeline(read(out / 'events-source.jsonl.credit-timeline.json'), case['grant_target_bytes'], True, case['grant_target_seconds'])
        commits.add(host['commit'])
        reviewed.append(dict(case=case['id'], repeat=repeat, timeline=timeline, accounting=accounting,
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
