#!/bin/sh
# 05-04 incremental recovery regression. Not a full plan-completion verdict.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
out=${PHASE5_RECOVERY_EVIDENCE_DIR:-"$root/artifacts/phase5/recovery"}
mkdir -p "$out"
export PHASE5_RECOVERY_EVIDENCE_DIR="$out"
export PHASE5_AUTHORIZATION_EVIDENCE_DIR="$out"
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
python3 - "$VORTEX_TEST_FULL_DISK_DIR" <<'PYFIXTURE'
import os,sys
v=os.statvfs(sys.argv[1])
assert 0 < v.f_blocks*v.f_frsize <= 16*1024*1024, 'full-disk fixture must be an isolated small filesystem'
PYFIXTURE
go test -count=1 -race -v ./agent/store ./agent ./hub > "$out/recovery-race.log" 2>&1
go vet ./agent/... ./hub > "$out/vet.log" 2>&1
python3 tools/phase5/audit_recovery.py "$out" > "$out/audit.json"
cat "$out/audit.json"
