#!/bin/sh
set -eu
test "$(GOTOOLCHAIN=local go env GOVERSION)" = go1.27.3
# The Linux runner owns the bind mount as a different uid from container root.
# Trust only this exact checkout in the ephemeral container, retain Go's VCS
# stamping, and independently check the source identity before building.
git config --global --add safe.directory "$PWD"
test "$(git rev-parse HEAD)" = "$PHASE6_EXPECTED_SHA"
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
if test "${PHASE6_THROUGH_PLAN:-06-02}" = 06-03; then
  cd ..
  (
    GOTOOLCHAIN=local go test ./contracts ./agent/... ./cmd/vortex-node -p 1 -count=1 -v
    GOTOOLCHAIN=local go -C engine test ./... -count=1 -v
    GOTOOLCHAIN=local go test -race ./contracts ./agent/... ./cmd/vortex-node -p 1 -count=1 -v
    GOTOOLCHAIN=local go -C engine test -race ./... -count=1 -v
  ) > /evidence/rule-components.log 2>&1
  (
    for arch in amd64 arm64; do
      GOOS=linux GOARCH="$arch" CGO_ENABLED=0 GOTOOLCHAIN=local go -C backend build -trimpath -o "/tmp/vortex-backend-$arch" ./cmd/vortex-backend
      GOOS=linux GOARCH="$arch" CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -o "/tmp/vortex-agent-$arch" ./cmd/vortex-node
      GOOS=linux GOARCH="$arch" CGO_ENABLED=0 GOTOOLCHAIN=local go -C engine build ./...
      printf 'engine package compilation GOOS=linux GOARCH=%s CGO_ENABLED=0\n' "$arch"
      GOTOOLCHAIN=local go version -m "/tmp/vortex-backend-$arch" "/tmp/vortex-agent-$arch"
    done
  ) > /evidence/rule-builds.log 2>&1
fi
