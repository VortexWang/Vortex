#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
mode=${BACKEND:-native}
qual=${QUALIFICATION_DIR:-$root/artifacts/phase4/qualification/$mode}
bin=${VORTEX_RELAY_BIN:-$root/engine/vortex-relay}
mkdir -p "$root/artifacts/phase4" "$qual"

if [ "$mode" = native ]; then
  if [ ! -x "$bin" ]; then
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
  for name in source relay-1 relay-2 relay-3 destination; do
    if [ -f "$qual/$name.pid" ]; then kill "$(sed -n '1p' "$qual/$name.pid")" 2>/dev/null || true; fi
  done
}
trap cleanup INT TERM EXIT

sleep 5
python3 - "$qual/echo-target.log" <<'PY' &
import socket, sys
with socket.socket() as server:
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind(("127.0.0.1", 18081))
    server.listen(1)
    conn, _ = server.accept()
    with conn:
        payload = conn.recv(4096)
        conn.sendall(payload)
        open(sys.argv[1], "wb").write(payload)
PY
target_pid=$!
sleep 1
python3 - "$qual/client.out" <<'PY'
import socket, sys
with socket.create_connection(("127.0.0.1", 18080), 10) as client:
    client.sendall(b"vortex-three-relay-smoke")
    client.shutdown(socket.SHUT_WR)
    data = client.recv(4096)
open(sys.argv[1], "wb").write(data)
if data != b"vortex-three-relay-smoke":
    raise SystemExit("echo payload mismatch")
PY
kill "$target_pid" 2>/dev/null || true
for name in source relay-1 relay-2 relay-3 destination; do
  grep -q 'OPEN authorized' "$qual/logs/$name.log"
done
printf 'status=pass\nmode=%s\npayload=vortex-three-relay-smoke\n' "$mode" >"$qual/result.txt"
