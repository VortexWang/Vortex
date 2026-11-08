#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
export PYTHONDONTWRITEBYTECODE=1
test "$(protoc --version)" = 'libprotoc 36.2'
test "$(protoc-gen-go --version)" = 'protoc-gen-go v1.36.12'
test "$(protoc-gen-go-grpc --version)" = 'protoc-gen-go-grpc 1.6.2'
work=$(mktemp -d /tmp/vortex-limits-contract.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
# Validate the exact approved historical snapshot using its original checker.
git clone --quiet --shared --no-checkout "$root" "$work/history"
git -C "$work/history" checkout --quiet --detach 84821a04cec4dec78a8a3b192d2003d1f44518bc
(cd "$work/history"; sh tools/phase6/rule_contract_check.sh)
for pass in one two; do
 mkdir "$work/$pass"
 protoc -I . --go_out="$work/$pass" --go_opt=paths=source_relative --go-grpc_out="$work/$pass" --go-grpc_opt=paths=source_relative contracts/protobuf/v1/vortex.proto
done
for name in vortex.pb.go vortex_grpc.pb.go; do
 cmp "$work/one/contracts/protobuf/v1/$name" "$work/two/contracts/protobuf/v1/$name"
 cmp "$work/one/contracts/protobuf/v1/$name" "contracts/protobuf/v1/$name"
done
python3 tools/phase6/limits_contract_check.py "$work"
echo 'Limits contract: two clean generations and exact historical approvals passed; CR-008 approval checked independently'
