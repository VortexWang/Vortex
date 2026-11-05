#!/bin/sh
# Incremental 05-03 regression; does not certify full runtime authority.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
out=${PHASE5_AUTHORIZATION_EVIDENCE_DIR:-"$root/artifacts/phase5/authorization"}
mkdir -p "$out"
export PHASE5_AUTHORIZATION_EVIDENCE_DIR="$out"
sha=${VERIFY_SHA:-$(git rev-parse HEAD)}
case "$sha" in *[!0-9a-f]*|'') echo 'invalid source SHA' >&2; exit 1;; esac
[ ${#sha} -eq 40 ] || { echo 'source SHA must be full length' >&2; exit 1; }
finish() {
  code=$?
  trap - EXIT
  status=failed
  [ "$code" -ne 0 ] || status=passed
  printf '{"schema":1,"status":"%s","commit":"%s","scope":"incremental-durable-credit-gate","plan_complete":false}\n' "$status" "$sha" > "$out/report.json"
  cat "$out/report.json"
  exit "$code"
}
trap finish EXIT
{
  printf 'commit: %s\n' "$sha"
  go version
  uname -srm
  if [ -n "${VERIFY_SHA:-}" ]; then
    printf 'source: caller-supplied clean archive\n'
  else
    printf 'tracked_diff:\n'
    git diff --stat HEAD
  fi
} > "$out/host.txt"
go test -count=1 -race ./agent/... ./hub ./test/phase5 > "$out/agent-hub-race.log" 2>&1
go vet ./agent/... ./hub ./test/phase5 ./cmd/vortex-node > "$out/vet.log" 2>&1
(cd engine && go test -count=1 -race ./accounting ./runtime/...) > "$out/engine-race.log" 2>&1
