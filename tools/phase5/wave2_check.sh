#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
out=${PHASE5_EVIDENCE_DIR:-"$root/artifacts/phase5/wave2"}
mkdir -p "$out"
{
  printf 'commit: '; git -C "$root" rev-parse HEAD
  printf 'go: '; go version
  printf 'arch: '; uname -m
} > "$out/host.txt"
go test ./agent/... ./hub ./test/phase5 > "$out/go-test.log"
go vet ./agent/... ./hub ./test/phase5 > "$out/go-vet.log"
printf '{"schema":1,"status":"passed","commit":"%s","tests":"go test ./agent/... ./hub ./test/phase5","vet":"go vet ./agent/... ./hub ./test/phase5","scope":"registration-mtls-reconcile-upgrade"}\n' "$(git -C "$root" rev-parse HEAD)" > "$out/report.json"
cat "$out/report.json"
