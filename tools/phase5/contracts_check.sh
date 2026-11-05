#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"

test "$(protoc --version)" = "libprotoc 36.2"
test "$(protoc-gen-go --version)" = "protoc-gen-go v1.36.12"
test "$(protoc-gen-go-grpc --version)" = "protoc-gen-go-grpc 1.6.2"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/first" "$tmp/second"
for pass in first second; do
	protoc -I . \
		--go_out="$tmp/$pass" --go_opt=paths=source_relative \
		--go-grpc_out="$tmp/$pass" --go-grpc_opt=paths=source_relative \
		contracts/protobuf/v1/vortex.proto
done
cmp "$tmp/first/contracts/protobuf/v1/vortex.pb.go" "$tmp/second/contracts/protobuf/v1/vortex.pb.go"
cmp "$tmp/first/contracts/protobuf/v1/vortex_grpc.pb.go" "$tmp/second/contracts/protobuf/v1/vortex_grpc.pb.go"
cmp "$tmp/first/contracts/protobuf/v1/vortex.pb.go" contracts/protobuf/v1/vortex.pb.go
cmp "$tmp/first/contracts/protobuf/v1/vortex_grpc.pb.go" contracts/protobuf/v1/vortex_grpc.pb.go

python3 tools/phase1/contracts.py check-frozen --contract-id vortex.control.v1
python3 tools/phase1/contracts.py check-map
python3 -m unittest discover -s tools/phase1 -p 'test_contracts.py'

base=${CONTRACT_BASE:-HEAD^}
git cat-file -e "$base^{commit}"
python3 tools/phase1/contracts.py check-change --base "$base"
