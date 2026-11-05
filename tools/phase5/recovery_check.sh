#!/bin/sh
# 05-04 incremental recovery regression. Not a full plan-completion verdict.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
out=${PHASE5_RECOVERY_EVIDENCE_DIR:-"$root/artifacts/phase5/recovery"}
mkdir -p "$out"
export PHASE5_RECOVERY_EVIDENCE_DIR="$out"
sha=${VERIFY_SHA:-$(git rev-parse HEAD)}
case "$sha" in *[!0-9a-f]*|'') exit 1;; esac
[ ${#sha} -eq 40 ] || exit 1
finish() {
 code=$?
 trap - EXIT
 status=failed
 [ "$code" -ne 0 ] || status=passed
 printf '{"schema":1,"status":"%s","commit":"%s","scope":"activation-query-and-hub-handoff","plan_complete":false,"backend":"protocol-test-only"}\n' "$status" "$sha" > "$out/report.json"
 cat "$out/report.json"
 exit "$code"
}
trap finish EXIT
{
 printf 'commit: %s\n' "$sha"
 go version
 uname -srm
} > "$out/host.txt"
[ -n "${VORTEX_TEST_FULL_DISK_DIR:-}" ] || { echo 'isolated full-disk filesystem is required' >&2; exit 1; }
go test -count=1 -race -v ./agent/store ./agent ./hub > "$out/recovery-race.log" 2>&1
go vet ./agent/... ./hub > "$out/vet.log" 2>&1
python3 - "$out" <<'PY'
import gzip, hashlib, json, pathlib, sqlite3, sys, tempfile
out=pathlib.Path(sys.argv[1])
meta=json.loads((out/'recovery-hub.json').read_text())
raw=gzip.decompress((out/'recovery-hub.sqlite.gz').read_bytes())
assert hashlib.sha256(raw).hexdigest()==meta['SHA256']
with tempfile.TemporaryDirectory() as d:
 p=pathlib.Path(d)/'hub.db'; p.write_bytes(raw)
 db=sqlite3.connect(f'file:{p}?mode=ro',uri=True)
 assert db.execute('pragma integrity_check').fetchone()[0]=='ok'
 assert db.execute('select count(*) from hub_transfers').fetchone()[0]==1
 assert db.execute('select count(*) from hub_ledger where confirmed=0').fetchone()[0]==0
 assert db.execute('select signer_epoch from issuer_epochs').fetchone()[0]=='2'
 assert db.execute('select remaining_bytes from issuer_delegations').fetchone()[0]=='512'
 assert db.execute('select count(*) from issuer_issues').fetchone()[0]==2
 assert meta['Info']['JournalMode']=='wal' and meta['Info']['Synchronous']=='2'
log=(out/'recovery-race.log').read_text()
for name in ('TestHandoffFullDiskDoesNotPartiallyTransfer','TestHandoffSIGKILLRecovery','TestRecoveryReplaysDurablePrefixAndTailAfterLostACK','TestRecoveryQueriesExecutionBeforeReleasingResources'):
 assert f'--- PASS: {name}' in log, name
print('recovery ledger integrity, conservation and fault cases verified')
PY
