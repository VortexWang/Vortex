#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
ARCH=$(docker info --format '{{.Architecture}}')
case "$ARCH" in
  aarch64) ARCH=arm64 ;;
  x86_64) ARCH=amd64 ;;
esac
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/vortex-engine-test.XXXXXX")
IMAGE="vortex-engine-test:local-$$"
cleanup() {
	rm -f "$TMP_DIR/relay.test"
	rmdir "$TMP_DIR" 2>/dev/null || true
  docker image rm "$IMAGE" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

cd "$ROOT/engine"
GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0 go test -c -o "$TMP_DIR/relay.test" ./relay
docker build --platform "linux/$ARCH" -f "$ROOT/test/engine/Dockerfile" -t "$IMAGE" "$TMP_DIR"
docker run --rm --platform "linux/$ARCH" "$IMAGE" -test.v
