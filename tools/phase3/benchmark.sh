#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
OUT_DIR="$ROOT/artifacts/phase3/benchmark"
RUN_ID=$(date -u +%Y%m%dT%H%M%SZ)
OUT="$OUT_DIR/$RUN_ID.txt"
mkdir -p "$OUT_DIR"
ARCH=$(docker info --format '{{.Architecture}}' 2>/dev/null || echo unavailable)
OS=$(docker info --format '{{.OperatingSystem}}' 2>/dev/null || echo unavailable)
COMMIT=$(git -C "$ROOT" rev-parse HEAD)
WORKTREE=clean; [ -z "$(git -C "$ROOT" status --porcelain)" ] || WORKTREE=dirty
{
	echo "status: environment-limited"
	echo "reason: Docker smoke is not dedicated hardware NFR qualification."
	echo "commit: $COMMIT"
	echo "worktree: $WORKTREE"
	echo "architecture: $ARCH"
	echo "docker_os: $OS"
	echo "forward_throughput_mbps: unmeasured"
	echo "reverse_delta_percent: unqualified"
	echo "first_packet_pool_ms: unqualified"
	echo "first_packet_mux_ms: unqualified"
} > "$OUT"
cat "$OUT"
