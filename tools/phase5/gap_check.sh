#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
out=${1:?evidence directory required}
sha=${2:?source SHA required}
python3 -m unittest discover -s tests/phase5
python3 tools/phase5/audit_recovery.py "$out/recovery" > "$out/audit.json"
python3 tools/phase5/audit_policy.py "$out/policy" > "$out/policy-audit.json"
python3 tools/phase5/audit_renewal_safety.py "$out/renewal-safety" > "$out/renewal-safety-audit.json"
python3 tools/phase5/audit_usage_retention.py "$out/retention" > "$out/retention-audit.json"
python3 tools/phase5/audit_gap_evidence.py --collect --out "$out" --sha "$sha"
