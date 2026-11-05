#!/bin/sh
# Run after qualification.sh on the same clean SHA. It restarts the exact route
# for independent TLS and runtime fault checks; none is a production ledger.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
mode=${BACKEND:-native}
runtime=${QUALIFICATION_DIR:-$root/artifacts/phase4/qualification/$mode}
out=${COMPARISON_DIR:-$root/artifacts/phase4/comparison/$mode}
mkdir -p "$out" "$root/artifacts/phase4"
out=$(CDPATH= cd -- "$out" && pwd)
rm -f "$out"/fault-ready "$out"/fault-injected "$out"/forged-open.json "$out"/middle-hop-fault.json "$out"/comparison.json "$out"/rtt-*.json
export BACKEND="$mode"
topology_pid= target_pid= fault_pid=
cleanup() {
  if [ -n "$fault_pid" ]; then kill "$fault_pid" 2>/dev/null || true; wait "$fault_pid" 2>/dev/null || true; fault_pid=; fi
  if [ -n "$topology_pid" ]; then kill "$topology_pid" 2>/dev/null || true; wait "$topology_pid" 2>/dev/null || true; topology_pid=; fi
  if [ "$mode" = docker ]; then
    QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" stop >/dev/null 2>&1 || true
    mkdir -p "$out/logs"
    for name in source relay-1 relay-2 relay-3 destination; do
      QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" logs --no-color "$name" >"$out/logs/$name.log" 2>&1 || true
    done
    QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" down --remove-orphans >/dev/null 2>&1 || true
  fi
  if [ -n "$target_pid" ]; then kill "$target_pid" 2>/dev/null || true; wait "$target_pid" 2>/dev/null || true; target_pid=; fi
}
trap cleanup EXIT INT TERM
start() {
  rm -f "$out/topology-ready"
  QUALIFICATION_DIR="$out" "$root/tools/phase4/run-topology.sh" >"$out/topology.log" 2>&1 &
  topology_pid=$!
  for attempt in $(seq 1 900); do
    test -f "$out/topology-ready" && break
    kill -0 "$topology_pid" 2>/dev/null || { cat "$out/topology.log" >&2; exit 1; }
    sleep 1
  done
  test -f "$out/topology-ready"
  if [ "$mode" = native ]; then
    python3 "$root/tools/phase4/echo_target.py" --host 127.0.0.1 >"$out/target.log" 2>&1 &
    target_pid=$!
  fi
  sleep 2
}

# First collect actual-runtime latency and forged OPEN/link-loss evidence.
start
QUALIFICATION_DIR="$out" "$root/tools/phase4/driver.sh" latency.py --output "$out/runtime-latency.json"
if [ "$mode" = native ]; then
  (cd "$root/engine" && go run ./cmd/phase4-probe --config "$out/config/source.json" --output "$out/forged-open.json")
else
  QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" exec -T source /usr/local/bin/phase4-probe --config /config/source.json --output "$out/forged-open.json"
fi
python3 "$root/tools/phase4/attack_gate.py" --report "$out/forged-open.json" --output "$out/attack-gate.json"


for name in relay-2 destination; do
  if [ "$name" = relay-2 ]; then hop=1; port=19443; docker_port=9443; else hop=3; port=19445; docker_port=9445; fi
  if [ "$mode" = native ]; then
    (cd "$root/engine" && go run ./cmd/phase4-probe --config "$out/config/source.json" --bypass-peer "127.0.0.1:$port" --bypass-hop "$hop" --output "$out/bypass-$name.json")
  else
    QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" exec -T source /usr/local/bin/phase4-probe --config /config/source.json --bypass-peer "$name:$docker_port" --bypass-hop "$hop" --output "$out/bypass-$name.json"
  fi
done

# Maintain active TCP and UDP flows across an actual middle-process stop. The
# driver signals readiness only after both payloads traverse the complete route.
rm -f "$out/fault-ready" "$out/fault-injected"
QUALIFICATION_DIR="$out" "$root/tools/phase4/driver.sh" fault_workload.py "$out" >"$out/fault-driver.log" 2>&1 &
fault_pid=$!
for attempt in $(seq 1 90); do
  test -f "$out/fault-ready" && break
  kill -0 "$fault_pid" 2>/dev/null || { cat "$out/fault-driver.log" >&2; exit 1; }
  sleep 1
done
test -f "$out/fault-ready"
if [ "$mode" = native ]; then kill "$(cat "$out/relay-2.pid")"; else
  QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" stop relay-2 >/dev/null
fi
: > "$out/fault-injected"
wait "$fault_pid" || { cat "$out/fault-driver.log" >&2; exit 1; }
fault_pid=
cleanup
mkdir -p "$out/runtime-logs"
cp "$out"/logs/*.log "$out/runtime-logs/"
cp "$out/host.json" "$out/runtime-host.json"
cp "$out/semantic-manifest.json" "$out/runtime-manifest.json"

if [ "$mode" = native ]; then
  (cd "$root/engine" && go build -o "$root/artifacts/phase4/tls-baseline" ./cmd/tls-baseline)
  export VORTEX_RELAY_BIN="$root/artifacts/phase4/tls-baseline"
else
  export QUALIFICATION_BINARY=/usr/local/bin/tls-baseline
fi
export QUALIFICATION_READY_MESSAGE='minimal-tls ready'
start
QUALIFICATION_DIR="$out" "$root/tools/phase4/driver.sh" latency.py --output "$out/tls-latency.json"
for name in source relay-1 relay-2 relay-3 destination; do
  probe=tls; if [ "$name" = destination ]; then probe=target; fi
  if [ "$mode" = native ]; then
    "$VORTEX_RELAY_BIN" --config "$out/config/$name.json" --probe "$probe" --output "$out/rtt-$name.json"
  else
    QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" exec -T "$name" /usr/local/bin/tls-baseline --config "/config/$name.json" --probe "$probe" --output "$out/rtt-$name.json"
  fi
done
QUALIFICATION_DIR="$out" "$root/tools/phase4/driver.sh" mixed_workload.py --tcp-only --duration "${MIXED_DURATION_SECONDS:-5}" --tcp-streams "${MIXED_TCP_STREAMS:-4}" --output "$out/tls-throughput.json"
QUALIFICATION_DIR="$out" "$root/tools/phase4/driver.sh" mixed_workload.py --duration "${MIXED_DURATION_SECONDS:-5}" --tcp-streams "${MIXED_TCP_STREAMS:-4}" --udp-mbps "${MIXED_UDP_MBPS:-100}" --output "$out/tls-mixed-throughput.json"
cleanup
python3 "$root/tools/phase4/compare_results.py" "$runtime" "$out"
trap - EXIT INT TERM
