#!/bin/sh
# Validate the exact committed source while preserving the caller's dirty tree.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
sha=${PHASE6_EXPECTED_SHA:-$(git rev-parse HEAD)}
test "$(git rev-parse HEAD)" = "$sha"
work=$(mktemp -d /tmp/vortex-phase6-clean.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
git clone --quiet --no-hardlinks "$root" "$work/source"
git -C "$work/source" checkout --quiet --detach "$sha"
test -z "$(git -C "$work/source" status --porcelain=v1)"
export PHASE6_EXPECTED_SHA="$sha"
export PHASE6_SOURCE_ROOT="$work/source"
export PHASE6_EVIDENCE_DIR=${PHASE6_EVIDENCE_DIR:-"$root/artifacts/phase6/06-01/$sha/docker"}
mkdir -p "$PHASE6_EVIDENCE_DIR"
git status --porcelain=v1 > "$PHASE6_EVIDENCE_DIR/caller-status.txt"
git -C "$work/source" status --porcelain=v1 > "$PHASE6_EVIDENCE_DIR/source-status-before.txt"
git -C "$work/source" rev-parse HEAD HEAD^{tree} > "$PHASE6_EVIDENCE_DIR/source-identity.txt"
"$work/source/tools/phase6/tracer_real.sh"
test -z "$(git -C "$work/source" status --porcelain=v1)"
git -C "$work/source" status --porcelain=v1 > "$PHASE6_EVIDENCE_DIR/source-status-after.txt"
