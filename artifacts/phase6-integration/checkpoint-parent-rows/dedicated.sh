#!/bin/bash
set -u
cd /opt/vortex-acceptance
mkdir -p parent-update
set +e
docker run --rm -e GOMAXPROCS=2 -v /opt/vortex-acceptance/candidate-dd1beabd:/src:ro -v /opt/vortex-acceptance/parent-update:/diagnostic -v vortex-acceptance-gomod:/go/pkg/mod -v vortex-acceptance-gocache:/root/.cache/go-build -w /src golang:1.27.3@sha256:c292963ad0e5f51e843c726edc204a4e4c26a859ba1e4d736d91028aa3633048 go test -p 1 -race -count=1 -overlay=/diagnostic/dedicated-overlay.json ./agent/store -run 'Test(RuntimeCheckpoint|RuntimeBlockCheckpoint|CrossRequestCheckpoints)' -v > parent-update/dedicated-race.log 2>&1
rc=$?
printf '%s\n' "$rc" > parent-update/dedicated-race.exit
exit "$rc"
