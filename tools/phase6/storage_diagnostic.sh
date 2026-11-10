#!/bin/sh
# An isolated observation of the unchanged production code. Not final evidence.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
out=${1:?absolute external output directory required}
mkdir -p "$out/bin"
image=golang:1.27.3@sha256:c292963ad0e5f51e843c726edc204a4e4c26a859ba1e4d736d91028aa3633048
git rev-parse HEAD HEAD^{tree} > "$out/source.txt"
git diff --name-only 9dd03bc219917606d4e766b629d4df37b0a87e03 HEAD > "$out/diagnostic-only-diff.txt"
findmnt --json > "$out/host-mounts.json"
docker info --format '{{json .}}' > "$out/docker-info.json"
docker run --rm -v "$root:/src:ro" -v "$out:/evidence" -v phase6-diag-mod:/go/pkg/mod -v phase6-diag-build:/root/.cache/go-build -w /src -e PHASE6_DIAGNOSTIC_CPU_ONLY="${PHASE6_DIAGNOSTIC_CPU_ONLY:-0}" -e GOTOOLCHAIN=local -e GOFLAGS=-p=1 -e GOMAXPROCS=2 "$image" sh -c '
 set -eu
 git config --global --add safe.directory /src
 go build -race -o /evidence/bin/vortex-node ./cmd/vortex-node &&
 go build -race -o /evidence/bin/vortex-test-hub ./cmd/vortex-test-hub
 if test "$PHASE6_DIAGNOSTIC_CPU_ONLY" = 1; then
  mv /evidence/bin/vortex-node /evidence/bin/vortex-node-real
  cp tools/phase6/profile_node.sh /evidence/bin/vortex-node
  chmod 755 /evidence/bin/vortex-node
  go version -m /evidence/bin/vortex-node-real > /evidence/node-build-info.txt
 fi
' > "$out/build.log" 2>&1
status=0
for location in docker-root temporary-disk; do
 dir=$out/$location
 mkdir "$dir"
 if test "$location" = temporary-disk; then
  if ! python3 -c 'import os,sys; sys.exit(0 if os.path.isdir("/mnt") and os.stat("/mnt").st_dev != os.stat("/var/lib/docker").st_dev else 1)'; then
   printf '%s\n' 'No separate /mnt device; comparison unavailable, not a pass or required-test skip.' > "$dir/unavailable.txt"
   continue
  fi
  case $(findmnt -n -T /mnt -o FSTYPE) in
   ext4|xfs|btrfs|zfs) ;;
   *) printf '%s\n' 'No supported durable local filesystem at /mnt.' > "$dir/unavailable.txt"; continue;;
  esac
  backing=$(sudo mktemp -d /mnt/vortex-storage-diagnostic.XXXXXX)
  volume=$(docker volume create --driver local --opt type=none --opt o=bind --opt "device=$backing")
 else
  volume=$(docker volume create)
 fi
 docker volume inspect "$volume" > "$dir/volume.json"
 code=0
 docker run --rm -v "$root:/src:ro" -v "$out:/evidence" -v "$volume:/runtime" -v phase6-diag-mod:/go/pkg/mod -v phase6-diag-build:/root/.cache/go-build -w /src -e VORTEX_DIAGNOSTIC_CPU_ONLY="${PHASE6_DIAGNOSTIC_CPU_ONLY:-0}" -e GOTOOLCHAIN=local -e GOFLAGS=-p=1 -e GOMAXPROCS=2 "$image" sh -c '
  set -eu
  git config --global --add safe.directory /src
  mkdir -p /runtime/tmp /runtime/evidence
  observer=
  finish() {
   code=$?
   trap - EXIT
   if test -n "$observer"; then
    kill "$observer" 2>/dev/null || true
    wait "$observer" 2>/dev/null || true
   fi
   printf "%s\n" "$code" > /runtime/evidence/test-exit.txt
   for profile in /runtime/evidence/profiles/*/cpu.pprof; do
    test -f "$profile" || continue
    profile_code=0
    go tool pprof -top -nodecount=60 /evidence/bin/vortex-node-real "$profile" > "$profile.top.txt" 2>&1 || profile_code=$?
    go tool pprof -top -cum -nodecount=60 /evidence/bin/vortex-node-real "$profile" > "$profile.cumulative.txt" 2>&1 || profile_code=$?
    printf "%s\n" "$profile_code" > "$profile.parse-exit.txt"
   done
   cp -a /runtime/evidence/. "/evidence/$1/" || exit $?
   chown -R "$2" /evidence || exit $?
   exit "$code"
  }
  location=$1
  owner=$2
  trap '\''finish "$location" "$owner"'\'' EXIT
  export TMPDIR=/runtime/tmp VORTEX_PROCESS_BIN_DIR=/evidence/bin VORTEX_PROCESS_EVIDENCE_DIR=/runtime/evidence/processes
  mkdir -p "$VORTEX_PROCESS_EVIDENCE_DIR"
  cat /proc/self/mountinfo > /runtime/evidence/mountinfo.txt
  python3 tools/phase6/storage_observer.py fsync /runtime > /runtime/evidence/fsync.json
  python3 tools/phase6/storage_observer.py processes > /runtime/evidence/process-counters.jsonl &
  observer=$!
  code=0
  selection="^(TestRuntimeFundingBlockContinuousTraffic|TestDurableAuthorityProcesses(TCP|UDP|ConcurrentTCP|ConcurrentRenewal))$"
  if test "$VORTEX_DIAGNOSTIC_CPU_ONLY" = 1; then selection="^TestDurableAuthorityProcessesConcurrentRenewal$"; fi
  printf "%s\n" "$selection" > /runtime/evidence/test-selection.txt
  go test -race -json ./agent -count=1 -timeout=10m -run "$selection" > /runtime/evidence/tests.jsonl 2>&1 || code=$?
  exit "$code"
 ' storage-diagnostic "$location" "$(id -u):$(id -g)" || code=$?
 test "$code" -eq 0 || status=1
 # Keep volume names and all failed bytes for the life of this diagnostic VM.
done
printf '{"scope":"diagnostic only; not same-SHA acceptance","test_exit":%s}\n' "$status" > "$out/result.json"
exit "$status"
