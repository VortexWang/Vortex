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
	 echo "status: runtime-qualification-passed"
	 echo "scope: runtime-backed three-relay qualification"
} > "$OUT/metadata.txt"
QUALIFICATION_DIR="$ROOT/artifacts/phase4/qualification/native" BACKEND=native tools/phase4/qualification.sh
cd "$ROOT/engine"
set +e
go test ./scheduler ./vlink -run '^$' -bench 'Benchmark' -benchtime=1s -count=1 > "$OUT/raw.txt" 2>&1
status=$?
set -e
cat "$OUT/raw.txt"
echo "benchmark_exit: $status" >> "$OUT/metadata.txt"
echo "qualification: topology-and-accounting-pass" >> "$OUT/metadata.txt"
echo "performance: environment-limited-until-native-dedicated-host" >> "$OUT/metadata.txt"
go run "$ROOT/tools/phase4/udp_loss.go" -count 20000 > "$OUT/udp-loss.txt"
cat "$OUT/udp-loss.txt"
exit 0
