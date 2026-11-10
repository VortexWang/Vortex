#!/bin/sh
# Validate exact original approvals AND independently approved current bytes.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
export PYTHONDONTWRITEBYTECODE=1
if test ! -f docs/governance/proposals/CR-012-phase6-integration.json; then
 exec python3 tools/phase6/rest_contract_check.py
fi
work=$(mktemp -d /tmp/vortex-cr012-history.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
git clone --quiet --shared "$root" "$work/source"
git -C "$work/source" checkout --quiet --detach c85ed753c8ea92965346f12f5b4e7ea30e900695
(cd "$work/source"; python3 tools/phase6/rest_contract_check.py)
python3 tools/phase6/integration_contract_check.py --require-approval
