#!/bin/sh
set -eu

mode=${BACKEND:-native}
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
out=${QUALIFICATION_DIR:-$root/artifacts/phase4/qualification/$mode}
"$root/tools/phase4/generate-qualification.sh" "$mode" "$out"

if [ "$mode" = docker ]; then
	docker compose -f "$root/test/qualification/compose.yaml" up --build -d
	for attempt in $(seq 1 180); do
		running=$(QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" ps --status running --services | wc -l | tr -d ' ')
		if [ "$running" -ge 6 ]; then break; fi
		sleep 1
	done
	if [ "$running" -lt 6 ]; then
		echo "qualification topology failed to start all containers" >&2
		docker compose -f "$root/test/qualification/compose.yaml" logs >&2
		exit 1
	fi
	: > "$out/topology-ready"
	while :; do sleep 3600; done
fi

if [ -n "${VORTEX_RELAY_BIN:-}" ]; then
	bin=$VORTEX_RELAY_BIN
	if [ ! -x "$bin" ]; then
		echo "configured relay binary is not executable: $bin" >&2
		exit 1
	fi
else
	bin=$root/engine/vortex-relay
  (cd "$root/engine" && go build -o "$bin" ./cmd/vortex-relay)
fi
mkdir -p "$out/logs"
for name in source relay-1 relay-2 relay-3 destination; do
  "$bin" --config "$out/config/$name.json" >"$out/logs/$name.log" 2>&1 &
  echo $! >"$out/$name.pid"
done
trap 'for name in source relay-1 relay-2 relay-3 destination; do test -f "$out/$name.pid" && kill "$(cat "$out/$name.pid")" 2>/dev/null || true; done' INT TERM EXIT
for attempt in $(seq 1 30); do
	ready=1
	for name in source relay-1 relay-2 relay-3 destination; do
		grep -q 'vortex-relay ready' "$out/logs/$name.log" || ready=0
	done
	if [ "$ready" -eq 1 ]; then break; fi
	sleep 1
done
if [ "$ready" -ne 1 ]; then
	echo "qualification topology failed to start all relay processes" >&2
	exit 1
fi
: > "$out/topology-ready"
wait
