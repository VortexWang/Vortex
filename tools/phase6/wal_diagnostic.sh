#!/bin/sh
# Diagnostic only: WAL NOOP counters and syscall timing; not acceptance.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
out=${1:?outside source output directory}
mkdir -p "$out"
git rev-parse HEAD HEAD^{tree} > "$out/source.txt"
volume=$(docker volume create)
docker volume inspect "$volume" > "$out/volume.json"
code=0
docker run --rm --cap-add SYS_PTRACE --security-opt seccomp=unconfined -v "$root:/src:ro" -v "$out:/output" -v "$volume:/runtime" -w /src -e GOTOOLCHAIN=local -e GOFLAGS=-p=1 -e GOMAXPROCS=2 golang:1.27.3@sha256:c292963ad0e5f51e843c726edc204a4e4c26a859ba1e4d736d91028aa3633048 sh -c '
 set -eu
 git config --global --add safe.directory /src
 apt-get update >/output/apt.log 2>&1
 apt-get install -y --no-install-recommends strace >>/output/apt.log 2>&1
 dpkg-query -W strace >/output/strace-version.txt
 mkdir -p /runtime/bin /runtime/tmp /runtime/evidence
 finish() { code=$?; trap - EXIT; cp -a /runtime/evidence/. /output/; exit "$code"; }
 trap finish EXIT
 export TMPDIR=/runtime/tmp
 for mode in normal race; do
  flags=
  test "$mode" != race || flags=-race
  export VORTEX_PROCESS_BIN_DIR=/runtime/bin VORTEX_PROCESS_EVIDENCE_DIR=/runtime/evidence/process-$mode
  mkdir -p "$VORTEX_PROCESS_EVIDENCE_DIR"
  go build $flags -o /runtime/bin/vortex-node ./cmd/vortex-node
  go build $flags -o /runtime/bin/vortex-test-hub ./cmd/vortex-test-hub
  # Compile before syscall observation to isolate runtime activity.
  go test $flags -c -o /runtime/bin/agent.test ./agent
  status=0
  strace -f -ttt -T -yy -e trace=fsync,fdatasync,pwrite64 -o /runtime/evidence/syscalls-$mode.log /runtime/bin/agent.test -test.v -test.run="^(TestRuntimeLongLivedDurableAdaptiveRenewal|TestRuntimeFundingBlockContinuousTraffic|TestDurableAuthorityProcesses(TCP|UDP|ConcurrentTCP|ConcurrentRenewal))$" -test.count=1 -test.timeout=10m > /runtime/evidence/tests-$mode.log 2>&1 || status=$?
  printf "%s\n" "$status" > /runtime/evidence/exit-$mode.txt
 done
' > "$out/runner.log" 2>&1 || code=$?
printf '{"scope":"diagnostic only, traced and instrumented; not acceptance","exit":%s}\n' "$code" > "$out/result.json"
exit "$code"
