#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
sha=${PHASE6_EXPECTED_SHA:-$(git rev-parse HEAD)}
export PHASE6_EXPECTED_SHA="$sha"
export PHASE6_THROUGH_PLAN=${PHASE6_THROUGH_PLAN:-06-02}
export PHASE6_EVIDENCE_DIR=${PHASE6_EVIDENCE_DIR:-"$root/artifacts/phase6/06-01/$sha/docker"}
mkdir -p "$PHASE6_EVIDENCE_DIR"
export PHASE6_SOURCE_ROOT=${PHASE6_SOURCE_ROOT:-"$root"}
export PHASE6_DATABASE_URL="postgres://vortex:phase6-local-test-only@127.0.0.1:${PHASE6_PG_PORT:-15436}/vortex?sslmode=disable"
export PHASE6_VALKEY_ADDR="127.0.0.1:${PHASE6_VALKEY_PORT:-16386}"
compose="$root/deploy/backend/compose.test.yml"
docker compose -f "$compose" up -d --wait postgres valkey
docker compose -f "$compose" config > "$PHASE6_EVIDENCE_DIR/compose.yml"
docker version > "$PHASE6_EVIDENCE_DIR/docker-version.txt"
docker info --format '{{json .}}' > "$PHASE6_EVIDENCE_DIR/docker-info.json"
docker compose -f "$compose" images --format json > "$PHASE6_EVIDENCE_DIR/images.json"
docker compose -f "$compose" exec -T postgres psql -U vortex -c 'SELECT version();' > "$PHASE6_EVIDENCE_DIR/postgres-version.txt"
docker compose -f "$compose" exec -T valkey valkey-cli INFO server > "$PHASE6_EVIDENCE_DIR/valkey-version.txt"
# Capture the exit status before printing the log; no pipeline may hide failure.
result=0
docker compose -f "$compose" run --rm backend-test > "$PHASE6_EVIDENCE_DIR/tracer.log" 2>&1 || result=$?
cat "$PHASE6_EVIDENCE_DIR/tracer.log"
clean=false
if test -z "$(git -C "$PHASE6_SOURCE_ROOT" status --porcelain=v1)"; then clean=true; fi
printf '{"commit":"%s","result":%s,"source":"%s","source_clean":%s,"actions":"not_run"}\n' "$sha" "$result" "$PHASE6_SOURCE_ROOT" "$clean" > "$PHASE6_EVIDENCE_DIR/result.json"
exit "$result"
