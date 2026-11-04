#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
mode=${BACKEND:-native}
qual=${QUALIFICATION_DIR:-$root/artifacts/phase4/qualification/$mode}
bin=${VORTEX_RELAY_BIN:-$root/artifacts/phase4/vortex-relay}
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
  kill "$topology_pid" 2>/dev/null || true
  wait "$topology_pid" 2>/dev/null || true
  if [ "$mode" = docker ]; then
    QUALIFICATION_DIR="$qual" docker compose -f "$root/test/qualification/compose.yaml" stop >/dev/null 2>&1 || true
    mkdir -p "$qual/logs"
    for name in source relay-1 relay-2 relay-3 destination; do
      QUALIFICATION_DIR="$qual" docker compose -f "$root/test/qualification/compose.yaml" logs --no-color "$name" >"$qual/logs/$name.log" 2>&1 || true
    done
    QUALIFICATION_DIR="$qual" docker compose -f "$root/test/qualification/compose.yaml" down --remove-orphans >/dev/null 2>&1 || true
  fi
  if [ -n "${target_pid:-}" ]; then kill "$target_pid" 2>/dev/null || true; fi
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
python3 - "$qual/client.out" "$qual/udp-client.out" <<'PY'
import socket, sys
with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
    client.settimeout(10)
    client.sendto(b"vortex-three-relay-udp-smoke", ("127.0.0.1", 18082))
    response, _ = client.recvfrom(4096)
if response != b"vortex-three-relay-udp-smoke":
    raise SystemExit("udp echo payload mismatch")
open(sys.argv[2], "wb").write(response)
with socket.create_connection(("127.0.0.1", 18080), 10) as client:
    client.sendall(b"vortex-three-relay-smoke")
    client.shutdown(socket.SHUT_WR)
    data = client.recv(4096)
open(sys.argv[1], "wb").write(data)
if data != b"vortex-three-relay-smoke":
    raise SystemExit("echo payload mismatch")
PY
capture_container_udp start
python3 "$root/tools/phase4/mixed_workload.py" \
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
