#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
OUT="$ROOT/artifacts/phase4/benchmark"
mkdir -p "$OUT"
commit=$(git -C "$ROOT" rev-parse HEAD)
arch=$(docker info --format '{{.Architecture}}' 2>/dev/null || uname -m)
{
  echo "commit: $commit"
  echo "architecture: $arch"
  echo "status: environment-limited"
  echo "reason: dedicated native three-relay qualification host is not asserted by this command"
  echo "scope: engine mixed-load and scheduler benchmark smoke"
} > "$OUT/metadata.txt"
cd "$ROOT/engine"
go test ./scheduler ./vlink -run '^$' -bench 'Benchmark' -benchtime=1s -count=1 | tee "$OUT/raw.txt"
