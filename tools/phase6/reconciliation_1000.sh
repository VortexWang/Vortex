#!/bin/sh
# Real PG backend qualification with an explicitly deterministic Node adapter.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
: "${PHASE6_DATABASE_URL:?real PostgreSQL required}"
: "${PHASE6_RECONCILIATION_EVIDENCE:?absolute output JSON outside source required}"
case "$PHASE6_RECONCILIATION_EVIDENCE" in
 "$root"/*) echo 'evidence must be outside source' >&2; exit 1;;
 /*) ;;
 *) echo 'absolute evidence path required' >&2; exit 1;;
esac
test ! -e "$PHASE6_RECONCILIATION_EVIDENCE"
export GOTOOLCHAIN=local
export PHASE6_EXPECTED_SHA=${PHASE6_EXPECTED_SHA:-$(git rev-parse HEAD)}
test "$(go env GOVERSION)" = go1.27.3
# Go returns nonzero for partial, timeout, wrong identity or >60s; JSON persists
# failed timing evidence too. An unavailable PG is a failure, never a skip.
go -C backend test ./internal/reconciliation ./internal/worker ./internal/service -run 'TestReconcile1000Within60s|TestWithin60s|TestRuleReconciliation' -count=1 -v
python3 - "$PHASE6_RECONCILIATION_EVIDENCE" "$PHASE6_EXPECTED_SHA" <<'PY'
import datetime,hashlib,json,re,sys
p,sha=sys.argv[1:];r=json.load(open(p))
assert r['commit']==sha and re.fullmatch('[a-f0-9]{40}',sha)
assert r['status']=='passed' and (r['rules'],r['resolved'],r['members'],r['observed'])==(1000,1000,2000,2000)
assert 0<=r['elapsed_ns']<=60000000000 and r['elapsed_ms']==r['elapsed_ns']/1000000
assert re.fullmatch('[a-f0-9]{64}',r['inventory_sha256'])
start,end=(datetime.datetime.fromisoformat(r[k].replace('Z','+00:00')) for k in ['started_at','ended_at'])
assert abs((end-start).total_seconds()*1e9-r['elapsed_ns'])<10000000
print('1000-rule backend reconciliation passed; no Engine/transport performance claim')
PY
