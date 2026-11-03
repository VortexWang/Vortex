#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
OUT_DIR=${VORTEX_PERF_OUTPUT_DIR:-"$ROOT/artifacts/phase3/benchmark"}
RUN_ID=$(date -u +%Y%m%dT%H%M%SZ)
OUT="$OUT_DIR/$RUN_ID.txt"
mkdir -p "$OUT_DIR"
ARCH=$(docker info --format '{{.Architecture}}' 2>/dev/null || uname -m)
case "$ARCH" in aarch64|arm64) ARCH=arm64 ;; x86_64|amd64) ARCH=amd64 ;; esac
OS=$(docker info --format '{{.OperatingSystem}}' 2>/dev/null || true)
if [ -z "$OS" ] && [ -r /etc/os-release ]; then . /etc/os-release; OS=$PRETTY_NAME; fi
if [ -z "$OS" ]; then OS=$(sw_vers -productVersion 2>/dev/null | sed 's/^/macOS /' || echo unknown); fi
COMMIT=${VORTEX_PERF_COMMIT:-$(git -C "$ROOT" rev-parse HEAD)}
WORKTREE=${VORTEX_PERF_WORKTREE:-clean}
if [ -z "${VORTEX_PERF_WORKTREE:-}" ] && [ -n "$(git -C "$ROOT" status --porcelain)" ]; then WORKTREE=dirty; fi
BENCHTIME=${VORTEX_PERF_BENCHTIME:-7x}
BENCHCOUNT=${VORTEX_PERF_BENCHCOUNT:-3}
RAW="$OUT.raw.txt"
GO_VERSION=$(cd "$ROOT/engine" && go version 2>&1 || true)
CPU_SUMMARY=$(lscpu 2>/dev/null | awk -F: '/^Model name:/ {gsub(/^[ \t]+/, "", $2); print $2; exit}')
if [ -z "$CPU_SUMMARY" ]; then CPU_SUMMARY=$(sysctl -n machdep.cpu.brand_string 2>/dev/null || uname -m); fi
if [ -r /proc/meminfo ]; then
	MEMORY_KIB=$(awk '/MemTotal:/ {print $2}' /proc/meminfo)
else
	MEMORY_KIB=$(sysctl -n hw.memsize 2>/dev/null || echo unavailable)
fi
VIRTUALIZATION=$(systemd-detect-virt 2>/dev/null || echo unknown)
{
	echo "status: measured"
	echo "qualification: numeric candidate measurements; full system acceptance requires approved path topology and workload"
	echo "commit: $COMMIT"
	echo "worktree: $WORKTREE"
	echo "architecture: $ARCH"
	echo "docker_os: $OS"
	echo "kernel: $(uname -r)"
	echo "virtualization: $VIRTUALIZATION"
	echo "cpu_summary: $CPU_SUMMARY"
	echo "cpu_count: $(getconf _NPROCESSORS_ONLN)"
	echo "memory_kib: $MEMORY_KIB"
	echo "go_version: $GO_VERSION"
	echo "topology: loopback TCP pairs; measured path does not include external network hops"
	echo "workload: single 64 MiB tracer or Mux transfer per sample; production accounting gate disabled"
	echo "benchmark: go test ./vlink -run '^$' -bench '^BenchmarkVLink(TCPThroughput|FirstByte)$' -benchtime=$BENCHTIME -count=$BENCHCOUNT"
} > "$OUT"
set +e
(cd "$ROOT/engine" && go test ./vlink -run '^$' -bench '^BenchmarkVLink(TCPThroughput|FirstByte)$' -benchtime="$BENCHTIME" -count="$BENCHCOUNT") > "$RAW" 2>&1
BENCH_EXIT=$?
set -e
if [ "$BENCH_EXIT" -ne 0 ]; then
	cat "$RAW" >> "$OUT"
	echo "benchmark_status: failed"
	cat "$OUT"
	exit "$BENCH_EXIT"
fi
metric() {
	awk -v benchmark="$1" -v unit="$2" '$1 ~ benchmark { for (i = 1; i <= NF; i++) if ($i == unit) { print $(i-1); break } }' "$RAW" | sort -n | awk '{ values[NR] = $1 } END { if (!NR) exit 1; if (NR % 2) print values[(NR + 1) / 2]; else printf "%.3f\n", (values[NR / 2] + values[NR / 2 + 1]) / 2 }'
}
FORWARD_MB=$(metric '^BenchmarkVLinkTCPThroughput/vlink-tracer/forward-' 'MB/s')
REVERSE_MB=$(metric '^BenchmarkVLinkTCPThroughput/vlink-tracer/reverse-' 'MB/s')
BASELINE_MB=$(metric '^BenchmarkVLinkTCPThroughput/tls-baseline-' 'MB/s')
MUX_FORWARD_MB=$(metric '^BenchmarkVLinkTCPThroughput/vlink-mux/forward-' 'MB/s')
MUX_REVERSE_MB=$(metric '^BenchmarkVLinkTCPThroughput/vlink-mux/reverse-established-' 'MB/s')
MUX_EXTRA_US=$(metric '^BenchmarkVLinkFirstByte/mux-' 'median-extra-us')
POOL_EXTRA_US=$(metric '^BenchmarkVLinkFirstByte/pool-prewarmed-' 'median-extra-us')
if [ -n "$FORWARD_MB" ] && [ -n "$REVERSE_MB" ] && [ -n "$BASELINE_MB" ]; then
	awk -v forward="$FORWARD_MB" -v reverse="$REVERSE_MB" -v baseline="$BASELINE_MB" -v mux_forward="$MUX_FORWARD_MB" -v mux_reverse="$MUX_REVERSE_MB" -v mux="$MUX_EXTRA_US" -v pool="$POOL_EXTRA_US" 'BEGIN {
		forward_mbps=forward*8; baseline_mbps=baseline*8; ratio=forward/baseline*100;
		delta=(forward > 0 ? (forward-reverse)/forward*100 : 0); if (delta < 0) delta=-delta;
		printf "NFR-002 forward_mbps: %.2f\n", forward_mbps;
		printf "NFR-002 baseline_mbps: %.2f\n", baseline_mbps;
		printf "NFR-002 tunnel_to_baseline_percent: %.2f\n", ratio;
		printf "NFR-002 measured_status: %s\n", (forward_mbps >= 600 && ratio >= 70 ? "candidate-pass" : "candidate-fail");
		printf "transfer_direction_delta_percent: %.2f\n", delta;
		if (mux_forward != "" && mux_reverse != "") {
			mux_fwd_mbps=mux_forward*8; mux_rev_mbps=mux_reverse*8; mux_delta=(mux_fwd_mbps > 0 ? (mux_fwd_mbps-mux_rev_mbps)/mux_fwd_mbps*100 : 100); if (mux_delta < 0) mux_delta=-mux_delta;
			printf "NFR-004 forward_established_mbps: %.2f\n", mux_fwd_mbps;
			printf "NFR-004 reverse_established_mbps: %.2f\n", mux_rev_mbps;
			printf "NFR-004 direction_delta_percent: %.2f\n", mux_delta;
			printf "NFR-004 measured_status: %s\n", (mux_delta <= 5 ? "candidate-pass" : "candidate-fail");
		} else {
			printf "NFR-004 qualification: unqualified (reverse-established Mux measurements missing)\n";
		}
		printf "transfer_direction_delta_percent: %.2f (diagnostic only; one forward-established link)\n", delta;
		printf "NFR-007 mux_median_extra_us: %s\n", mux;
		printf "NFR-007 mux_measured_status: %s\n", (mux != "" && mux <= 5000 ? "candidate-pass" : "candidate-fail");
		printf "NFR-007 pool_median_extra_us: %s\n", pool;
		printf "NFR-007 pool_measured_status: %s\n", (pool != "" && pool <= 5000 ? "candidate-pass" : "candidate-fail");
	}' >> "$OUT"
else
	echo 'measurement_parse_status: incomplete' >> "$OUT"
fi
cat "$RAW" >> "$OUT"
echo "benchmark_status: passed" >> "$OUT"
cat "$OUT"
