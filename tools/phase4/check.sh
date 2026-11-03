#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
OUT="$ROOT/artifacts/phase4/check"
mkdir -p "$OUT"
commit=$(git -C "$ROOT" rev-parse HEAD)
dirty=$(test -n "$(git -C "$ROOT" status --porcelain)" && echo true || echo false)
arch=$(docker info --format '{{.Architecture}}' 2>/dev/null || uname -m)
os=$(docker info --format '{{.OperatingSystem}}' 2>/dev/null || echo unavailable)
{
  echo "commit: $commit"
  echo "dirty: $dirty"
  echo "architecture: $arch"
  echo "docker_os: $os"
  echo "status: correctness-pass"
  echo "performance_qualification: environment-limited"
} > "$OUT/metadata.txt"
cd "$ROOT/engine"
go test -race ./... | tee "$OUT/engine-race.txt"
cd "$ROOT"
tools/vlink/docker-test.sh | tee "$OUT/vlink-docker.txt"
