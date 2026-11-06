#!/bin/sh
set -eu
test "$(GOTOOLCHAIN=local go env GOVERSION)" = go1.27.3
go version
uname -a
date -u '+%Y-%m-%dT%H:%M:%SZ'
tools=/tmp/phase6-tools
sh tools/phase6/install_sqlc.sh "$tools"
export PATH="$tools:$PATH"
test "$(sqlc version)" = v1.31.1
cd backend
# Generate twice from independent input directories, then compare committed output.
work=$(mktemp -d /tmp/phase6-sqlc.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir "$work/one" "$work/two"
cp -R db/migrations db/query db/sqlc.yaml "$work/one/"
cp -R db/migrations db/query db/sqlc.yaml "$work/two/"
sqlc generate -f "$work/one/sqlc.yaml"
sqlc generate -f "$work/two/sqlc.yaml"
diff -ru "$work/one/generated" "$work/two/generated"
diff -ru db/generated "$work/one/generated"
sqlc generate -f db/sqlc.yaml
# Local replacement modules are source identities, not downloadable zip hashes.
# Verify the external graph independently; the clean-export gate checks both
# local modules against the exact commit and their own committed go.sum files.
mkdir "$work/verify"
cp go.mod go.sum "$work/verify/"
GOTOOLCHAIN=local go -C "$work/verify" mod edit -droprequire=vortex -droprequire=vortex/engine
GOTOOLCHAIN=local go -C "$work/verify" mod verify
GOTOOLCHAIN=local go build -trimpath -o /tmp/vortex-backend ./cmd/vortex-backend
GOTOOLCHAIN=local go build -trimpath -o /tmp/vortex-control-peer ./cmd/vortex-control-peer
GOTOOLCHAIN=local go version -m /tmp/vortex-backend /tmp/vortex-control-peer > /evidence/build-metadata.txt
export PHASE6_BACKEND_BINARY=/tmp/vortex-backend
export PHASE6_PEER_BINARY=/tmp/vortex-control-peer
/tmp/vortex-backend migrate
GOTOOLCHAIN=local go test ./... -count=1 -v
GOTOOLCHAIN=local go build -race -trimpath -o /tmp/vortex-backend ./cmd/vortex-backend
GOTOOLCHAIN=local go build -race -trimpath -o /tmp/vortex-control-peer ./cmd/vortex-control-peer
export GORACE=halt_on_error=1
GOTOOLCHAIN=local go test -race ./... -count=1 -v
