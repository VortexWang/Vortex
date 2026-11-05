#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
out="$root/artifacts/phase5/wave1"
mkdir -p "$out"
{
  printf 'commit: '; git -C "$root" rev-parse HEAD
  printf 'go: '; go version
  printf 'arch: '; uname -m
  printf 'sqlite module: '; go list -m modernc.org/sqlite
} > "$out/host.txt"
go test ./... > "$out/go-test.log"
go vet ./... > "$out/go-vet.log"
protoc -I "$root" --descriptor_set_out="$out/control-v1.pb" --include_imports contracts/protobuf/v1/vortex.proto
printf '{"schema":1,"status":"passed","commit":"%s","tests":"go test ./...","vet":"go vet ./...","control_descriptor":"%s"}\n' "$(git -C "$root" rev-parse HEAD)" "$out/control-v1.pb" > "$out/report.json"
cat "$out/report.json"
