#!/bin/sh
# Independent environment diagnosis. Never use these targeted runs as acceptance.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
out=${1:?outside-source evidence directory}
candidate=dd1beabd56db6f0abeb101b5266996f820bb7e81
mkdir -p "$out"
git rev-parse HEAD HEAD^{tree} > "$out/driver-source.txt"
findmnt --json > "$out/host-mounts.json"
lsblk --json -o NAME,TYPE,SIZE,FSTYPE,MOUNTPOINTS,MODEL > "$out/host-disks.json"
docker info --format '{{json .}}' > "$out/docker-info.json"
# A full local clone gives the container real VCS metadata for the exact product.
git clone --quiet --local --no-hardlinks "$root" "$out/candidate"
git -C "$out/candidate" checkout --quiet --detach "$candidate"
git -C "$out/candidate" rev-parse HEAD HEAD^{tree} > "$out/candidate-source.txt"
git -C "$out/candidate" status --porcelain=v1 > "$out/candidate-status-before.txt"
volume=$(docker volume create)
docker volume inspect "$volume" > "$out/root-volume.json"
data="$out/no-separate-data-disk"
mkdir -p "$data"
if test -d /mnt && test "$(stat -c %d /mnt)" != "$(stat -c %d /var/lib/docker)"; then
 data=$(sudo mktemp -d /mnt/vortex-disk-diagnostic.XXXXXXXX)
 sudo chown "$(id -u):$(id -g)" "$data"
 printf '%s\n' "$data" > "$out/separate-data-disk.txt"
fi
code=0
docker run --rm -v "$out/candidate:/src:ro" -v "$out:/output" -v "$volume:/rootdisk" -v "$data:/datadisk" -w /src -e VORTEX_DIAGNOSTIC_OWNER="$(id -u):$(id -g)" -e GOTOOLCHAIN=local -e GOFLAGS=-p=1 -e GOMAXPROCS=2 golang:1.27.3@sha256:c292963ad0e5f51e843c726edc204a4e4c26a859ba1e4d736d91028aa3633048 sh -c '
 set -eu
 git config --global --add safe.directory /src
 mkdir -p /rootdisk/bin /rootdisk/tmp /rootdisk/evidence /datadisk/tmp /datadisk/evidence
 finish() {
  code=$?; trap - EXIT
  mkdir -p /output/rootdisk /output/datadisk
  cp -a /rootdisk/evidence/. /output/rootdisk/ || code=1
  cp -a /datadisk/evidence/. /output/datadisk/ || code=1
  chown -R "$VORTEX_DIAGNOSTIC_OWNER" /output || code=1
  exit "$code"
 }
 trap finish EXIT
 cat /proc/self/mountinfo > /rootdisk/evidence/mounts.txt
 df -T /rootdisk /datadisk > /rootdisk/evidence/filesystems.txt
 export TMPDIR=/rootdisk/tmp
 # Compile before measuring runtime. No tracing, source instrumentation or PR changes.
 for mode in normal race; do
  flags=
  test "$mode" != race || flags=-race
  mkdir -p /rootdisk/bin/$mode
  go build $flags -o /rootdisk/bin/$mode/vortex-node ./cmd/vortex-node
  go build $flags -o /rootdisk/bin/$mode/vortex-test-hub ./cmd/vortex-test-hub
  go test $flags -c -o /rootdisk/bin/$mode/agent.test ./agent
 done
 disks=rootdisk
 test ! -f /output/separate-data-disk.txt || disks="rootdisk datadisk"
 for disk in $disks; do
  export TMPDIR=/$disk/tmp
  for mode in normal race; do
   export VORTEX_PROCESS_BIN_DIR=/rootdisk/bin/$mode VORTEX_PROCESS_EVIDENCE_DIR=/$disk/evidence/process-$mode
   mkdir -p "$VORTEX_PROCESS_EVIDENCE_DIR"
   status=0
   /rootdisk/bin/$mode/agent.test -test.v -test.run="^(TestRuntimeLongLivedDurableAdaptiveRenewal|TestRuntimeFundingBlockContinuousTraffic|TestDurableAuthorityProcesses(TCP|UDP|ConcurrentTCP|ConcurrentRenewal))$" -test.count=1 -test.timeout=10m > /$disk/evidence/tests-$mode.log 2>&1 || status=$?
   printf "%s\n" "$status" > /$disk/evidence/exit-$mode.txt
  done
 done
' > "$out/runner.log" 2>&1 || code=$?
git -C "$out/candidate" status --porcelain=v1 > "$out/candidate-status-after.txt"
# Do not upload a redundant full repository or its historical credentials.
rm -rf "$out/candidate"
printf '{"scope":"targeted disk diagnosis only; not acceptance","driver_exit":%s}\n' "$code" > "$out/result.json"
exit "$code"
