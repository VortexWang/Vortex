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
  if grep -q 'sustained control frame rate exceeded\|repeated .* violations' "$qual/logs/$name.log"; then
    echo "qualification blocked: protocol violation threshold reached in $name" >&2
    exit 1
  fi
done
test "$(cat "$qual/client.out")" = "vortex-three-relay-smoke"
test "$(cat "$qual/udp-client.out")" = "vortex-three-relay-udp-smoke"

python3 - "$qual/events-destination.jsonl" <<'PY'
import json, sys

totals = {}
count = 0
with open(sys.argv[1], encoding="utf-8") as events:
    for line in events:
        event = json.loads(line)
        if event["outcome"] != "committed" or event["reserved"] != event["used"]:
            raise SystemExit("accounting contains non-committed or unresolved smoke usage")
        totals[event["direction"]] = totals.get(event["direction"], 0) + event["used"]
        count += 1
expected = len(b"vortex-three-relay-smoke") + len(b"vortex-three-relay-udp-smoke")
if count < 4 or totals != {"left_to_right": expected, "right_to_left": expected}:
    raise SystemExit(f"accounting mismatch: events={count} totals={totals} expected_each={expected}")
PY

events=0
for file in "$qual"/events-*.jsonl; do
  if test -s "$file"; then events=$((events + 1)); fi
done
if test "$events" -lt 1; then
  echo "qualification blocked: accounting evidence is missing" >&2
  exit 1
fi
printf 'qualification=tcp-udp-topology-and-accounting-pass\nevent_files=%s\n' "$events" >"$qual/qualification.txt"
