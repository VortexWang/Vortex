#!/bin/sh
set -eu

mode=${BACKEND:-native}
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
out=${QUALIFICATION_DIR:-$root/artifacts/phase4/qualification/$mode}
"$root/tools/phase4/generate-qualification.sh" "$mode" "$out"

if [ "$mode" = docker ]; then
  exec docker compose -f "$root/test/qualification/compose.yaml" up --build
fi

bin=${VORTEX_RELAY_BIN:-$root/engine/vortex-relay}
if [ ! -x "$bin" ]; then
  (cd "$root/engine" && go build -o "$bin" ./cmd/vortex-relay)
fi
mkdir -p "$out/logs"
for name in source relay-1 relay-2 relay-3 destination; do
  "$bin" --config "$out/config/$name.json" >"$out/logs/$name.log" 2>&1 &
  echo $! >"$out/$name.pid"
done
trap 'for name in source relay-1 relay-2 relay-3 destination; do test -f "$out/$name.pid" && kill "$(cat "$out/$name.pid")" 2>/dev/null || true; done' INT TERM EXIT
wait
