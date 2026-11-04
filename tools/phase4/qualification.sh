#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
mode=${BACKEND:-native}
qual=${QUALIFICATION_DIR:-$root/artifacts/phase4/qualification/$mode}
VORTEX_RELAY_BIN=${VORTEX_RELAY_BIN:-$root/engine/vortex-relay}
export BACKEND="$mode" QUALIFICATION_DIR="$qual" VORTEX_RELAY_BIN

"$root/tools/phase4/topology-smoke.sh"

for name in source relay-1 relay-2 relay-3 destination; do
  test -s "$qual/logs/$name.log"
  grep -q 'OPEN authorized' "$qual/logs/$name.log"
done

events=0
for file in "$qual"/events-*.jsonl; do
  if test -s "$file"; then events=$((events + 1)); fi
done
if test "$events" -lt 1; then
  echo "qualification blocked: accounting evidence is missing" >&2
  exit 1
fi
printf 'qualification=topology-and-accounting-pass\nevent_files=%s\n' "$events" >"$qual/qualification.txt"
