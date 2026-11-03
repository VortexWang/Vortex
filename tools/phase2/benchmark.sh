#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
ARCH=$(docker info --format '{{.Architecture}}')
case "$ARCH" in aarch64) ARCH=arm64 ;; x86_64) ARCH=amd64 ;; arm64|amd64) ;; *) exit 2 ;; esac
CPUS=$(docker info --format '{{.NCPU}}')
MEMORY=$(docker info --format '{{.MemTotal}}')
KERNEL=$(docker info --format '{{.KernelVersion}}')
OS=$(docker info --format '{{.OperatingSystem}}')
DURATION=${VORTEX_BENCH_DURATION:-15s}
RUN_ID=$(date -u +%Y%m%dT%H%M%SZ)
OUT_DIR="$ROOT/artifacts/phase2/benchmark"
OUT="$OUT_DIR/$RUN_ID.txt"
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/vortex-phase2-benchmark.XXXXXX")
IMAGE="vortex-engine-benchmark:local-$$"
mkdir -p "$OUT_DIR"
cleanup() {
	rm -f "$TMP_DIR/benchmark.test" "$TMP_DIR/raw.txt"
	rmdir "$TMP_DIR" 2>/dev/null || true
	docker image rm "$IMAGE" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
cd "$ROOT/engine"
GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0 go test -c -o "$TMP_DIR/benchmark.test" ./relay
docker build --platform "linux/$ARCH" -f "$ROOT/test/engine/benchmark.Dockerfile" -t "$IMAGE" "$TMP_DIR" >/dev/null
COMMIT=$(git -C "$ROOT" rev-parse HEAD)
WORKTREE=clean
if [ -n "$(git -C "$ROOT" status --porcelain)" ]; then WORKTREE=dirty; fi
SOURCE_STATUS=clean
if [ -n "$(git -C "$ROOT" status --porcelain -- engine/relay/tcp.go engine/relay/tcp_linux.go engine/relay/tcp_portable.go engine/relay/tcp_benchmark_test.go)" ]; then SOURCE_STATUS=dirty; fi
SOURCE_DIGEST=$(cd "$ROOT" && shasum -a 256 engine/relay/tcp.go engine/relay/tcp_linux.go engine/relay/tcp_portable.go engine/relay/tcp_benchmark_test.go | shasum -a 256 | awk '{print $1}')
REASON="Docker-hosted measurements are development evidence; dedicated hardware qualification has not been asserted."
case "$OS" in *OrbStack*) REASON="OrbStack virtualization is not qualified performance hardware." ;; esac
if [ "$CPUS" -lt 4 ] || [ "$MEMORY" -lt 8589934592 ]; then REASON="$REASON Docker host is below the 4-vCPU/8-GiB reference capacity."; fi
printf 'status: environment-limited\nreason: %s\ncommit: %s\nworktree: %s\nbenchmark_sources: %s\nsource_digest_sha256: %s\ntopology: host-local TCP loopback client-to-relay-to-sink\ndocker_os: %s\narchitecture: %s\nkernel: %s\ncpus: %s\nmemory_bytes: %s\nduration: %s\n' "$REASON" "$COMMIT" "$WORKTREE" "$SOURCE_STATUS" "$SOURCE_DIGEST" "$OS" "$ARCH" "$KERNEL" "$CPUS" "$MEMORY" "$DURATION" > "$OUT"
printf 'command: docker run --rm --platform linux/%s %s -test.run=^$ -test.bench=^BenchmarkTCPRelayBareTCP$ -test.benchtime=%s -test.count=1\n\n--- raw benchmark output ---\n' "$ARCH" "$IMAGE" "$DURATION" >> "$OUT"
set +e
docker run --rm --platform "linux/$ARCH" "$IMAGE" -test.run='^$' -test.bench='^BenchmarkTCPRelayBareTCP$' -test.benchtime="$DURATION" -test.count=1 > "$TMP_DIR/raw.txt" 2>&1
BENCH_EXIT=$?
set -e
cat "$TMP_DIR/raw.txt" >> "$OUT"
if [ "$BENCH_EXIT" -eq 0 ]; then
	awk '$NF == "MB/s" { value = $(NF - 1) * 8; found = 1 } END { if (found) { printf "measurement: measured\nthroughput_mbps: %.2f\ndifference_from_700_mbps: %+.2f\n", value, value - 700 } else { print "measurement: unmeasured\nreason: benchmark output did not include MB/s"; exit 1 } }' "$TMP_DIR/raw.txt" >> "$OUT" || true
else
	printf 'measurement: unmeasured\nreason: benchmark exited with status %s\n' "$BENCH_EXIT" >> "$OUT"
fi
cat "$OUT"
printf '\nBenchmark evidence: %s\n' "$OUT"
exit "$BENCH_EXIT"
