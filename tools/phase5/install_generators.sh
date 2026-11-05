#!/bin/sh
# Installs only the previously locked generators into an isolated Linux prefix.
set -eu
prefix=${1:?isolated install prefix required}
mkdir -p "$prefix"
case "$(uname -m)" in
 x86_64) platform=linux-amd64; asset=linux-x86_64;;
 aarch64|arm64) platform=linux-arm64; asset=linux-aarch_64;;
 *) echo 'unsupported Linux architecture' >&2; exit 1;;
esac
curl --fail --location --retry 3 "https://github.com/protocolbuffers/protobuf/releases/download/v36.2/protoc-36.2-$asset.zip" --output "$prefix/protoc.zip"
python3 - "$prefix" "$platform" <<'PY'
import hashlib,json,pathlib,sys,zipfile
prefix=pathlib.Path(sys.argv[1]);platform=sys.argv[2]
lock=json.loads(pathlib.Path('docs/governance/version-candidates.yaml').read_text())
# The candidate catalog is the previously approved release integrity source.
rows=lock.get('candidates', lock.get('dependencies', []))
if not rows:
 rows=next(v for v in lock.values() if isinstance(v,list) and any(isinstance(r,dict) and r.get('name')=='protoc' for r in v))
entry=next(r for r in rows if r.get('name')=='protoc')
assert entry['version']=='36.2'
assert hashlib.sha256((prefix/'protoc.zip').read_bytes()).hexdigest()==entry['integrity_by_platform'][platform]
with zipfile.ZipFile(prefix/'protoc.zip') as z:
 z.extractall(prefix)
(prefix/'bin/protoc').chmod(0o755)
PY
GOBIN="$prefix/bin" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
GOBIN="$prefix/bin" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
