#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
OUT="$ROOT/artifacts/phase4/benchmark"
BACKEND=${BACKEND:-native}
QUALIFICATION_DIR=${QUALIFICATION_DIR:-$ROOT/artifacts/phase4/qualification/$BACKEND}
mkdir -p "$OUT"
commit=$(git -C "$ROOT" rev-parse HEAD)
arch=$(docker info --format '{{.Architecture}}' 2>/dev/null || uname -m)
{
  echo "commit: $commit"
  echo "architecture: $arch"
	 echo "runtime_qualification: pending"
	 echo "performance_qualification: not-qualified"
	 echo "scope: three-relay TCP baseline and mixed TCP/UDP runtime load, accounting, library benchmark"
} > "$OUT/metadata.txt"
BACKEND="$BACKEND" QUALIFICATION_DIR="$QUALIFICATION_DIR" tools/phase4/qualification.sh
echo "runtime_qualification: tcp-udp-mixed-runtime-and-accounting-pass" >> "$OUT/metadata.txt"
cd "$ROOT/engine"
set +e
go test ./scheduler ./vlink -run '^$' -bench 'Benchmark' -benchtime=1s -count=1 > "$OUT/raw.txt" 2>&1
status=$?
set -e
cat "$OUT/raw.txt"
echo "benchmark_exit: $status" >> "$OUT/metadata.txt"
echo "library_benchmark: completed" >> "$OUT/metadata.txt"
echo "performance_qualification: not-qualified" >> "$OUT/metadata.txt"
go run "$ROOT/tools/phase4/udp_loss.go" -count 20000 > "$OUT/udp-loss.txt"
cat "$OUT/udp-loss.txt"
exit "$status"
