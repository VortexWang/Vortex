#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
ARCH=$(docker info --format '{{.Architecture}}')
case "$ARCH" in aarch64) ARCH=arm64 ;; x86_64) ARCH=amd64 ;; arm64|amd64) ;; *) echo "unsupported Docker architecture: $ARCH" >&2; exit 2 ;; esac
IMAGE="vortex-vlink-check:local-$$"
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/vortex-vlink.XXXXXX")
cleanup() { rm -f "$TMP_DIR/vlink.test"; rmdir "$TMP_DIR" 2>/dev/null || true; docker image rm "$IMAGE" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

cd "$ROOT/engine"
GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0 go test -c -o "$TMP_DIR/vlink.test" ./vlink
docker build --platform "linux/$ARCH" -f "$ROOT/test/engine/vlink.Dockerfile" -t "$IMAGE" "$TMP_DIR" >/dev/null
docker run --rm --platform "linux/$ARCH" "$IMAGE" /vlink.test -test.v -test.run='Test(Tracer|Open|Target|UDP|Pool|Mode|Reverse|Cleanup|Limit|Observability|VLinkIntegration)'
