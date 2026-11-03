#!/bin/sh
set -eu

NETWORK=vortex-phase1-smoke-$$
SERVER=vortex-phase1-server-$$
CLIENT=vortex-phase1-client-$$
cleanup() {
  docker rm -f "$CLIENT" "$SERVER" >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

OS=$(docker info --format '{{.OSType}}')
[ "$OS" = linux ] || { echo '{"status":"skipped","reason":"Docker server is not Linux"}'; exit 0; }
docker network create --driver bridge "$NETWORK" >/dev/null
docker run -d --name "$SERVER" --network "$NETWORK" vortex-phase1:local -mode server -listen 0.0.0.0:9090 -message vortex-phase1 >/dev/null
sleep 1
docker run --rm --name "$CLIENT" --network "$NETWORK" vortex-phase1:local -mode client -address "$SERVER:9090" -message vortex-phase1 | grep -Fx 'vortex-phase1'
docker rm -f "$SERVER" >/dev/null
docker network rm "$NETWORK" >/dev/null
trap - EXIT INT TERM
echo '{"status":"passed","kind":"network-fixture-smoke","engine_acceptance":false}'
