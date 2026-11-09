#!/bin/sh
# Bounded 06-07 clean-source backend acceptance; final host pairing is06-11.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
: "${PHASE6_EXPECTED_SHA:?exact source SHA required}"
: "${PHASE6_DATABASE_URL:?real PostgreSQL required}"
out=${PHASE6_EVIDENCE_DIR:-/evidence}
case "$out" in "$root"|"$root"/*) echo 'evidence must be outside source' >&2; exit 1;; esac
mkdir -p "$out"
test ! -e "$out/source-identity.txt" || { echo 'refusing to overwrite evidence' >&2; exit 1; }
git config --global --add safe.directory "$root"
test "$(git rev-parse HEAD)" = "$PHASE6_EXPECTED_SHA"
test -z "$(git status --porcelain=v1)"
test "$(GOTOOLCHAIN=local go env GOVERSION)" = go1.27.3
git rev-parse HEAD HEAD^{tree} > "$out/source-identity.txt"
git status --porcelain=v1 > "$out/source-status-before.txt"
(go version; uname -a; date -u '+%Y-%m-%dT%H:%M:%SZ'; printf 'GOFLAGS=%s\nGOMAXPROCS=%s\n' "${GOFLAGS:-}" "${GOMAXPROCS:-}") > "$out/host.txt"
work=$(mktemp -d /tmp/vortex-rest-check.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
sh tools/phase6/install_sqlc.sh "$work/tools" > "$out/sqlc-install.log" 2>&1
export PATH="$work/tools:$PATH"
test "$(sqlc version)" = v1.31.1
for pass in one two; do
 mkdir "$work/$pass"
 cp -R backend/db/migrations backend/db/query backend/db/sqlc.yaml "$work/$pass/"
 sqlc generate -f "$work/$pass/sqlc.yaml"
done
diff -ru "$work/one/generated" "$work/two/generated" > "$out/sqlc-diff.log"
diff -ru backend/db/generated "$work/one/generated" >> "$out/sqlc-diff.log"
find "$work/one/generated" -type f -name '*.go' | wc -l > "$out/sqlc-output-count.txt"
cmp contracts/openapi/v1/openapi.yaml backend/openapi/v1/openapi.yaml
export GOTOOLCHAIN=local
export PHASE6_BACKEND_BINARY="$work/vortex-backend"
export PHASE6_PEER_BINARY="$work/vortex-control-peer"
go -C backend build -trimpath -o "$PHASE6_BACKEND_BINARY" ./cmd/vortex-backend
go -C backend build -trimpath -o "$PHASE6_PEER_BINARY" ./cmd/vortex-control-peer
go version -m "$PHASE6_BACKEND_BINARY" "$PHASE6_PEER_BINARY" > "$out/native-build-metadata.txt"
"$PHASE6_BACKEND_BINARY" migrate > "$out/migrate.log" 2>&1
PHASE6_RECONCILIATION_EVIDENCE="$out/reconciliation-normal.json" go -C backend test -json ./... -count=1 > "$out/backend-normal.jsonl" 2>&1
go -C backend build -race -trimpath -o "$PHASE6_BACKEND_BINARY" ./cmd/vortex-backend
go -C backend build -race -trimpath -o "$PHASE6_PEER_BINARY" ./cmd/vortex-control-peer
go version -m "$PHASE6_BACKEND_BINARY" "$PHASE6_PEER_BINARY" > "$out/race-build-metadata.txt"
export GORACE=halt_on_error=1
PHASE6_RECONCILIATION_EVIDENCE="$out/reconciliation-race.json" go -C backend test -json -race ./... -count=1 > "$out/backend-race.jsonl" 2>&1
for arch in amd64 arm64; do
 for target in vortex-backend vortex-hub; do
  GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go -C backend build -trimpath -o "$work/$target-$arch" ./cmd/"$target"
  go version -m "$work/$target-$arch"
 done
 GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -o "$work/vortex-node-$arch" ./cmd/vortex-node
 go version -m "$work/vortex-node-$arch"
done > "$out/cross-build-metadata.txt"
git status --porcelain=v1 > "$out/source-status-after.txt"
test -z "$(git status --porcelain=v1)"
test "$(git rev-parse HEAD)" = "$PHASE6_EXPECTED_SHA"
printf '%s\n' 'backend normal/race, two sqlc generations, six cross-builds and source cleanliness passed' > "$out/completion.txt"
