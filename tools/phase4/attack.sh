#!/bin/sh
# Native configured-route OPEN matrix, independent of the performance workload.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
out=${ATTACK_DIR:?set a fresh ATTACK_DIR}
[ ! -e "$out" ] || { echo 'refusing to overwrite attack evidence' >&2; exit 2; }
mkdir -p "$out"
out=$(CDPATH= cd -- "$out" && pwd)
git -C "$root" rev-parse HEAD >"$out/sha"
git -C "$root" status --porcelain >"$out/status"
topology_pid= target_pid=
cleanup() {
  if [ -n "$topology_pid" ]; then kill "$topology_pid" 2>/dev/null || true; wait "$topology_pid" 2>/dev/null || true; fi
  if [ -n "$target_pid" ]; then kill "$target_pid" 2>/dev/null || true; wait "$target_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
(cd "$root/engine" && go build -o "$out/phase4-probe" ./cmd/phase4-probe && go build -o "$out/vortex-relay" ./cmd/vortex-relay)
BACKEND=native PHASE4_FORMAL=1 VORTEX_RELAY_BIN="$out/vortex-relay" QUALIFICATION_DIR="$out" "$root/tools/phase4/run-topology.sh" >"$out/topology.log" 2>&1 &
topology_pid=$!
for attempt in $(seq 1 60); do
  test -f "$out/topology-ready" && break
  kill -0 "$topology_pid" 2>/dev/null || { cat "$out/topology.log" >&2; exit 1; }
  sleep 1
done
test -f "$out/topology-ready"
python3 "$root/tools/phase4/echo_target.py" --host 127.0.0.1 >"$out/target.log" 2>&1 &
target_pid=$!
sleep 1
kill -0 "$target_pid"
"$out/phase4-probe" --config "$out/config/source.json" --output "$out/forged-open.json" >"$out/probe.log" 2>&1
python3 "$root/tools/phase4/attack_gate.py" --report "$out/forged-open.json" --output "$out/attack-gate.json"
