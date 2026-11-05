#!/bin/sh
# Uses a clean committed checkout, never the caller's uncommitted worktree.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
sha=$(git rev-parse HEAD)
work=$(mktemp -d /tmp/vortex-phase5-check.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
git clone --quiet --no-hardlinks "$root" "$work/source"
git -C "$work/source" checkout --quiet --detach "$sha"
out=${PHASE5_DOCKER_EVIDENCE_DIR:-"$root/artifacts/phase5/check/$sha/docker"}
mkdir -p "$out"
out=$(CDPATH= cd -- "$out" && pwd)
docker run --rm --tmpfs /full-disk:rw,size=4m \
 -v "$work/source:/src:ro" -v "$out:/evidence" \
 -v vortex-phase5-gomod:/go/pkg/mod -v vortex-phase5-gocache:/root/.cache/go-build \
 -w /src -e VORTEX_TEST_FULL_DISK_DIR=/full-disk -e PHASE5_CHECK_EVIDENCE_DIR=/evidence \
 golang:1.27.3@sha256:c292963ad0e5f51e843c726edc204a4e4c26a859ba1e4d736d91028aa3633048 \
 sh -c 'git config --global --add safe.directory /src; sh tools/phase5/install_generators.sh /tmp/generators; export PATH=/tmp/generators/bin:$PATH; make phase5-check'
