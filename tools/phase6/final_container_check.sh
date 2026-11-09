#!/bin/sh
# Final Phase6 container suite on one exact clean source; outer host owns pairing.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
: "${PHASE6_EVIDENCE_DIR:?outside-source output required}"
out=$PHASE6_EVIDENCE_DIR
export PYTHONDONTWRITEBYTECODE=1
sh tools/phase6/rest_container_check.sh
for mode in normal race; do
 flags=
 test "$mode" != race || flags=-race
 go test $flags -json ./contracts ./agent/... ./hub ./cmd/vortex-node -p 1 -count=1 > "$out/root-$mode.jsonl" 2>&1
 go -C engine test $flags -json ./... -p 1 -count=1 > "$out/engine-$mode.jsonl" 2>&1
done
sh tools/phase6/ledger_fault_injection.sh > "$out/ledger-fault.log" 2>&1
sh tools/phase6/share_fault_injection.sh > "$out/share-fault.log" 2>&1
work=$(mktemp -d /tmp/vortex-phase6-final-generators.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
sh tools/phase5/install_generators.sh "$work/tools" > "$out/protobuf-install.log" 2>&1
export PATH="$work/tools/bin:$PATH"
for pass in one two; do
 mkdir "$work/$pass"
 protoc -I . --go_out="$work/$pass" --go_opt=paths=source_relative --go-grpc_out="$work/$pass" --go-grpc_opt=paths=source_relative contracts/protobuf/v1/vortex.proto
done
for file in vortex.pb.go vortex_grpc.pb.go; do
 cmp "$work/one/contracts/protobuf/v1/$file" "$work/two/contracts/protobuf/v1/$file"
 cmp "$work/one/contracts/protobuf/v1/$file" "contracts/protobuf/v1/$file"
done > "$out/protobuf-diff.log" 2>&1
python3 tools/phase6/rest_contract_check.py > "$out/exact-freeze.log" 2>&1
ruby tools/phase6/openapi_validate.rb contracts/openapi/v1/openapi.yaml > "$out/openapi.log" 2>&1
sh tools/phase6/dependency_lock_check.sh > "$out/dependencies.log" 2>&1
# Preserve the pre-existing Phase3 VLink review mismatch separately. It is not a
# Phase6 contract change or a passing historical global-freeze assertion.
legacy=0
python3 tools/phase1/contracts.py check-frozen --contract-id vortex.vlink.v1 > "$out/historical-vlink-freeze.log" 2>&1 || legacy=$?
printf '%s\n' "$legacy" > "$out/historical-vlink-freeze.exit"
git status --porcelain=v1 > "$out/source-status-after.txt"
test ! -s "$out/source-status-after.txt"
test "$(git rev-parse HEAD)" = "$PHASE6_EXPECTED_SHA"
printf '%s\n' 'phase6 backend/root/engine normal/race, finite1000 reconciliation, fault, generators, licenses and exact CR-009 freeze passed' > "$out/final-completion.txt"
