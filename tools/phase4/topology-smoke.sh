#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
mode=${BACKEND:-native}
qual=${QUALIFICATION_DIR:-$root/artifacts/phase4/qualification/$mode}
bin=${VORTEX_RELAY_BIN:-$root/artifacts/phase4/vortex-relay}
export BACKEND="$mode" QUALIFICATION_DIR="$qual"
target_pid=
mkdir -p "$root/artifacts/phase4" "$qual"
rm -f "$qual/topology-ready"

if [ "$mode" = native ]; then
  if [ -z "${VORTEX_RELAY_BIN:-}" ]; then
    (cd "$root/engine" && go build -o "$bin" ./cmd/vortex-relay)
  fi
  QUALIFICATION_DIR="$qual" BACKEND=native "$root/tools/phase4/run-topology.sh" >"$qual/topology.log" 2>&1 &
  topology_pid=$!
else
  QUALIFICATION_DIR="$qual" BACKEND=docker "$root/tools/phase4/run-topology.sh" >"$qual/topology.log" 2>&1 &
  topology_pid=$!
fi

cleanup() {
  cleanup_status=0
  kill "$topology_pid" 2>/dev/null || true
  wait "$topology_pid" 2>/dev/null || true
  if [ "$mode" = docker ]; then
    QUALIFICATION_DIR="$qual" docker compose -f "$root/test/qualification/compose.yaml" stop >/dev/null 2>&1 || cleanup_status=1
    mkdir -p "$qual/logs"
    for name in source relay-1 relay-2 relay-3 destination; do
      QUALIFICATION_DIR="$qual" docker compose -f "$root/test/qualification/compose.yaml" logs --no-color "$name" >"$qual/logs/$name.log" 2>&1 || true
      container=$(QUALIFICATION_DIR="$qual" docker compose -f "$root/test/qualification/compose.yaml" ps -a -q "$name")
      exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$container" 2>/dev/null) || exit_code=missing
      if [ "$exit_code" != 0 ]; then
        echo "qualification blocked: $name exited with status $exit_code; accounting evidence may be incomplete" >&2
        cleanup_status=1
      fi
    done
    QUALIFICATION_DIR="$qual" docker compose -f "$root/test/qualification/compose.yaml" down --remove-orphans >/dev/null 2>&1 || true
  fi
  if [ -n "${target_pid:-}" ]; then kill "$target_pid" 2>/dev/null || true; fi
  return "$cleanup_status"
}
trap cleanup INT TERM EXIT

capture_container_udp() {
  if [ "$mode" != docker ]; then return; fi
  for name in source destination target; do
    QUALIFICATION_DIR="$qual" docker compose -f "$root/test/qualification/compose.yaml" exec -T "$name" python3 -c '
import json
lines = open("/proc/net/snmp", encoding="utf-8").read().splitlines()
for i, line in enumerate(lines[:-1]):
    if line.startswith("Udp:"):
        print(json.dumps(dict(zip(line.split()[1:], map(int, lines[i+1].split()[1:]))), sort_keys=True))
        break
' >"$qual/kernel-$name-$1.json"
  done
}

for attempt in $(seq 1 "${TOPOLOGY_READY_TIMEOUT_SECONDS:-900}"); do
  test -f "$qual/topology-ready" && break
  if ! kill -0 "$topology_pid" 2>/dev/null; then
    cat "$qual/topology.log" >&2
    echo "qualification topology exited before readiness" >&2
    exit 1
  fi
  sleep 1
done
test -f "$qual/topology-ready" || { echo "qualification topology readiness timed out" >&2; exit 1; }
sleep 1
if [ "$mode" = native ]; then
  python3 "$root/tools/phase4/echo_target.py" --host 127.0.0.1 >"$qual/echo-target.log" 2>&1 &
  target_pid=$!
  sleep 1
fi
python3 - "$qual/target-start.json" <<'PY'
import json, sys, time, urllib.request
for attempt in range(60):
    try:
        with urllib.request.urlopen("http://127.0.0.1:18083/stats", timeout=3) as response:
            stats = json.load(response)
        break
    except (OSError, ValueError):
        if attempt == 59:
            raise
        time.sleep(0.5)
with open(sys.argv[1], "w", encoding="utf-8") as output:
    json.dump(stats, output, sort_keys=True)
    output.write("\n")
PY
if [ "${D04_SHORT_SAMPLES:-0}" != 0 ]; then
  "$root/tools/phase4/driver.sh" latency.py --samples "$D04_SHORT_SAMPLES" --output "$qual/short-flow-latency.json"
fi
"$root/tools/phase4/driver.sh" smoke_workload.py "$qual"
capture_container_udp start
"$root/tools/phase4/driver.sh" mixed_workload.py \
  --duration "${MIXED_DURATION_SECONDS:-5}" \
  --tcp-streams "${MIXED_TCP_STREAMS:-4}" \
  --udp-mbps "${MIXED_UDP_MBPS:-100}" \
  --output "$qual/mixed-workload.json"
capture_container_udp final
python3 - "$qual/target-final.json" <<'PY'
import json, sys, urllib.request
with urllib.request.urlopen("http://127.0.0.1:18083/stats", timeout=3) as response:
    stats = json.load(response)
with open(sys.argv[1], "w", encoding="utf-8") as output:
    json.dump(stats, output, sort_keys=True)
    output.write("\n")
PY
cleanup
trap - INT TERM EXIT
for name in source relay-1 relay-2 relay-3 destination; do
  grep -q 'OPEN authorized' "$qual/logs/$name.log"
done
printf 'status=pass\nmode=%s\npayload=vortex-three-relay-smoke\nmixed_workload=mixed-workload.json\n' "$mode" >"$qual/result.txt"
