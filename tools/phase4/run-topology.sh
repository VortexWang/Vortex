#!/bin/sh
set -eu

mode=${BACKEND:-native}
case "${PHASE4_TEST_ISSUER_RENEWAL_ONLY:-0}" in 0|1) ;; *) echo 'invalid renewal-only option' >&2; exit 2 ;; esac
if [ "${PHASE4_TEST_ISSUER_RENEWAL_ONLY:-0}" = 1 ] && [ -z "${PHASE4_TEST_ISSUER_DELAY:-}" ]; then
 echo 'renewal-only requires issuer delay' >&2; exit 2
fi
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
out=${QUALIFICATION_DIR:-$root/artifacts/phase4/qualification/$mode}
"$root/tools/phase4/generate-qualification.sh" "$mode" "$out"

if [ "$mode" = docker ] && [ -n "${PHASE4_TEST_ISSUER_DELAY:-}" ]; then
  echo "delayed issuer experiment currently requires native launcher" >&2; exit 2
fi

if [ "$mode" = docker ]; then
	compose_pid=
	wait_pid=
	stop_launcher() {
		trap - INT TERM
		if [ -n "$compose_pid" ]; then kill "$compose_pid" 2>/dev/null || true; wait "$compose_pid" 2>/dev/null || true; fi
		if [ -n "$wait_pid" ]; then kill "$wait_pid" 2>/dev/null || true; wait "$wait_pid" 2>/dev/null || true; fi
		exit 0
	}
	trap stop_launcher INT TERM
	docker compose -f "$root/test/qualification/compose.yaml" up --build -d &
	compose_pid=$!
	wait "$compose_pid"
	compose_pid=
	for attempt in $(seq 1 180); do
		running=$(QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" ps --status running --services | wc -l | tr -d ' ')
		if [ "$running" -ge 7 ]; then break; fi
		sleep 1
	done
	if [ "$running" -lt 7 ]; then
		echo "qualification topology failed to start all containers" >&2
		docker compose -f "$root/test/qualification/compose.yaml" logs >&2
		exit 1
	fi
	ready=0
 for attempt in $(seq 1 180); do
  ready=1
  for name in source relay-1 relay-2 relay-3 destination; do
   logs=$(QUALIFICATION_DIR="$out" docker compose -f "$root/test/qualification/compose.yaml" logs --no-color "$name")
   printf '%s\n' "$logs" | grep -q "${QUALIFICATION_READY_MESSAGE:-vortex-relay ready}" || ready=0
   if [ "${QUALIFICATION_READY_MESSAGE:-}" = "" ] && [ "$name" != source ] && [ "$name" != destination ]; then
    printf '%s\n' "$logs" | grep -q 'vlink next-hop ready' || ready=0
   fi
  done
  if [ "$ready" = 1 ]; then break; fi
  sleep 1
 done
 if [ "$ready" != 1 ]; then echo "topology role/link readiness failed" >&2; exit 1; fi
 : > "$out/topology-ready"
	while :; do sleep 3600 & wait_pid=$!; wait "$wait_pid"; done
fi

if [ -n "${VORTEX_RELAY_BIN:-}" ]; then
	bin=$VORTEX_RELAY_BIN
	if [ ! -x "$bin" ]; then
		echo "configured relay binary is not executable: $bin" >&2
		exit 1
	fi
else
	bin=$root/artifacts/phase4/vortex-relay
  mkdir -p "$root/artifacts/phase4"
  (cd "$root/engine" && go build -o "$bin" ./cmd/vortex-relay)
fi
mkdir -p "$out/logs"
for name in source relay-1 relay-2 relay-3 destination; do
  set -- --config "$out/config/$name.json"
  if [ "$name" = source ] && [ -n "${PHASE4_TEST_ISSUER_DELAY:-}" ]; then
    set -- "$@" --test-issuer-delay "$PHASE4_TEST_ISSUER_DELAY"
    if [ "${PHASE4_TEST_ISSUER_RENEWAL_ONLY:-0}" = 1 ]; then set -- "$@" --test-issuer-renewal-only; fi
  fi
  if [ "${CPU_PROFILE:-0}" = 1 ]; then
    "$bin" "$@" --cpu-profile "$out/$name.pprof" >"$out/logs/$name.log" 2>&1 &
  else
    "$bin" "$@" >"$out/logs/$name.log" 2>&1 &
  fi
  echo $! >"$out/$name.pid"
done
cleanup() {
  trap - INT TERM EXIT
  for name in source relay-1 relay-2 relay-3 destination; do
    test -f "$out/$name.pid" && kill "$(cat "$out/$name.pid")" 2>/dev/null || true
  done
  for name in source relay-1 relay-2 relay-3 destination; do
    test -f "$out/$name.pid" && wait "$(cat "$out/$name.pid")" 2>/dev/null || true
  done
}
trap cleanup INT TERM EXIT
for attempt in $(seq 1 30); do
	ready=1
	for name in source relay-1 relay-2 relay-3 destination; do
  grep -q "${QUALIFICATION_READY_MESSAGE:-vortex-relay ready}" "$out/logs/$name.log" || ready=0
  if [ "${QUALIFICATION_READY_MESSAGE:-}" = "" ] && [ "$name" != source ] && [ "$name" != destination ]; then
   grep -q 'vlink next-hop ready' "$out/logs/$name.log" || ready=0
  fi
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
