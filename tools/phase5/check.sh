#!/bin/sh
# Fail closed: Linux + real full-disk fixture + raw recovery evidence required.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
out=${PHASE5_CHECK_EVIDENCE_DIR:-"$root/artifacts/phase5/check"}
mkdir -p "$out"
sha=$(git rev-parse HEAD)
[ "$(uname -s)" = Linux ] || { echo 'phase5-check requires Linux' >&2; exit 1; }
git diff --quiet HEAD -- . ':(exclude)artifacts/**' || { echo 'phase5-check requires clean source' >&2; exit 1; }
[ -z "$(git ls-files --others --exclude-standard -- agent hub cmd contracts engine tools test tests model go.mod go.sum go.work go.work.sum Makefile ':/*.go' ':/*.s')" ] || { echo 'untracked source requires review' >&2; git ls-files --others --exclude-standard -- agent hub cmd contracts engine tools test tests model go.mod go.sum go.work go.work.sum Makefile ':/*.go' ':/*.s' >&2; exit 1; }
[ -n "${VORTEX_TEST_FULL_DISK_DIR:-}" ] || { echo 'isolated full-disk fixture required' >&2; exit 1; }
python3 - "$VORTEX_TEST_FULL_DISK_DIR" <<'PYFIXTURE'
import os,sys
v=os.statvfs(sys.argv[1])
assert 0 < v.f_blocks*v.f_frsize <= 16*1024*1024, 'full-disk fixture must be an isolated small filesystem'
PYFIXTURE
finish() {
 code=$?
 trap - EXIT
 status=failed
 [ "$code" -ne 0 ] || status=passed
 printf '{"schema":1,"status":"%s","commit":"%s","scope":"05-04-runtime-recovery","backend":"protocol-test-only","production_backend_authority":false}\n' "$status" "$sha" > "$out/report.json"
 cat "$out/report.json"
 exit "$code"
}
trap finish EXIT
sh tools/phase5/contracts_check.sh > "$out/contracts.log" 2>&1
go test -count=1 ./... > "$out/root-test.log" 2>&1
(cd engine && go test -count=1 ./...) > "$out/engine-test.log" 2>&1
(cd engine && go test -count=1 -race -v ./apply ./accounting ./runtime/...) > "$out/engine-race.log" 2>&1
PHASE5_RECOVERY_EVIDENCE_DIR="$out/recovery" sh tools/phase5/recovery_check.sh > "$out/recovery.log" 2>&1
python3 tools/phase5/audit_recovery.py "$out/recovery" > "$out/audit.json"
# Missing or skipped required faults cannot be turned into a passing report.
python3 - "$out" "$sha" <<'PY'
import json,pathlib,sys
out=pathlib.Path(sys.argv[1]);sha=sys.argv[2]
r=json.loads((out/'recovery/report.json').read_text())
assert r['status']=='passed' and r['commit']==sha and r['backend']=='protocol-test-only'
assert '--- PASS: TestConcurrentCommitAbortHasOneOutcome ' in (out/'engine-race.log').read_text()
for path in ('contracts.log','root-test.log','engine-test.log','engine-race.log','recovery/recovery-race.log','recovery/host.txt','audit.json'):
 assert (out/path).is_file() and (out/path).stat().st_size>0,path
PY
