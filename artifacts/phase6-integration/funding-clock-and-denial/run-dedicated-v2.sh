#!/bin/bash
set -u
base=/opt/vortex-acceptance/funding-clock-v2-20261113
mkdir -p "$base/evidence" "$base/bin"
docker run --rm -e GOMAXPROCS=2 -e TMPDIR=/runtime/tmp -e VORTEX_PROCESS_BIN_DIR=/runtime/bin -e VORTEX_PROCESS_EVIDENCE_DIR=/runtime/evidence -v /opt/vortex-acceptance/candidate-dd1beabd:/src:ro -v "$base":/runtime -v "$base/overlay":/overlay:ro -v vortex-acceptance-gomod:/go/pkg/mod -v vortex-acceptance-gocache:/root/.cache/go-build -w /src golang:1.27.3@sha256:c292963ad0e5f51e843c726edc204a4e4c26a859ba1e4d736d91028aa3633048 sh -c '
set -eu
mkdir -p /runtime/tmp
go build -p 1 -race -overlay=/overlay/overlay.json -o /runtime/bin/vortex-node ./cmd/vortex-node
go build -p 1 -race -overlay=/overlay/overlay.json -o /runtime/bin/vortex-test-hub ./cmd/vortex-test-hub
go test -p 1 -race -count=1 -overlay=/overlay/overlay.json ./agent/store ./agent -run "Test(FullyDeniedFundingGroupDoesNotCommitRolledBackPages|BlockFundingClockRefreshAfterWriterQueue|FundingBlockRefreshesClockAtDurableAdmission|RuntimeFundingBlockContinuousTraffic|DurableAuthorityProcesses(TCP|UDP|ConcurrentTCP|ConcurrentRenewal))$" -v
' > "$base/tests-race.log" 2>&1
rc=$?
printf '%s\n' "$rc" > "$base/exit.txt"
exit "$rc"
