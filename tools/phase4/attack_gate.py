#!/usr/bin/env python3
"""Check the enumerated configured-route attack evidence, not universal security."""
import argparse
import json
import re
from pathlib import Path

EXPECTED = {f'{protocol}/{name}': code for protocol in ('tcp', 'udp') for name, code in {
    'unknown-rule': 7, 'stale-revision': 7, 'stale-generation': 7,
    'future-revision': 7, 'future-generation': 7,
    'skipped-hop': 1, 'visited-local-loop': 1, 'duplicate-visited': 8}.items()}


def validate(report):
    reasons = []
    if not isinstance(report, dict) or report.get('status') != 'pass':
        return ['forged OPEN report did not pass']
    cases = report.get('cases')
    if not isinstance(cases, dict):
        return ['case evidence missing']
    for name, code in EXPECTED.items():
        case = cases.get(name)
        if not isinstance(case, dict) or case.get('status') != 'pass':
            reasons.append(f'{name}: missing or failed')
            continue
        if type(case.get('code')) is not int or case['code'] != code:
            reasons.append(f'{name}: incorrect rejection class')
        correlation = case.get('correlation_id')
        if not isinstance(correlation, str) or not re.fullmatch(r'[0-9a-f]{32}', correlation) or correlation == '0' * 32:
            reasons.append(f'{name}: invalid audit correlation')
    if not isinstance(cases.get('udp-link-loss'), dict) or cases['udp-link-loss'].get('status') != 'pass':
        reasons.append('udp-link-loss: missing or failed')
    return reasons


def main():
    parser = argparse.ArgumentParser(__doc__)
    parser.add_argument('--report', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    try:
        reasons = validate(json.loads(args.report.read_text()))
    except (OSError, ValueError):
        reasons = ['report missing or invalid']
    result = dict(schema=1, status='not-qualified' if reasons else 'pass', reasons=reasons,
                  required_cases=sorted(EXPECTED), scope='sixteen configured-route TCP/UDP forged OPEN cases plus UDP link loss; excludes identity handshake and durable authority matrices')
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True)+'\n')
    print(json.dumps(result, sort_keys=True))
    return bool(reasons)


if __name__ == '__main__':
    raise SystemExit(main())
