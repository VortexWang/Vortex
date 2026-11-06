#!/bin/sh
# Run on the host with Docker; root/Engine tests run separately in the pinned gate.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
sha=$(git rev-parse HEAD)
out=${PHASE5_DEPLOYMENT_EVIDENCE_DIR:-artifacts/phase5/deployment/$sha}
mkdir -p "$out"
image=vortex-node-test:$sha
docker build -f deploy/node/Dockerfile -t "$image" . > "$out/image-build.log" 2>&1
VORTEX_NODE_TEST_IMAGE="$image" go test -count=1 -tags nodecontainer -v ./test/phase5 -run '^TestNodeContainerLifecycle$' > "$out/container.log" 2>&1
for arch in amd64 arm64; do
 CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -o "$out/vortex-node-linux-$arch" ./cmd/vortex-node
 shasum -a 256 "$out/vortex-node-linux-$arch" >> "$out/binaries.sha256"
done
# Executable lookup is isolated in a temporary root; do not install to the host.
if command -v systemd-analyze >/dev/null 2>&1; then
 verify=$(mktemp -d)
 trap 'rm -rf "$verify"' EXIT HUP INT TERM
 mkdir -p "$verify/usr/local/bin" "$verify/etc/systemd/system"
 mkdir -p "$verify/usr/lib/systemd/system"
 # Minimal implicit target fixtures; this is a unit syntax/command check, not a boot claim.
 for target in sysinit basic shutdown; do
  printf '[Unit]\nDescription=Verification target\n' > "$verify/usr/lib/systemd/system/$target.target"
 done
 cp deploy/systemd/vortex-node.service deploy/systemd/vortex-node-upgrade.service "$verify/etc/systemd/system/"
 mkdir -p "$verify/usr/local/libexec"
 cp "$out/vortex-node-linux-amd64" "$verify/usr/local/libexec/vortex-node-upgrader"
 cp "$out/vortex-node-linux-amd64" "$verify/usr/local/bin/vortex-node"
 systemd-analyze verify --man=no --root="$verify" vortex-node.service vortex-node-upgrade.service > "$out/systemd-verify.log" 2>&1
else
 echo 'unavailable on this host; verify on Ubuntu Actions' > "$out/systemd-verify.log"
fi
python3 - "$out" "$sha" <<'PY'
import json,pathlib,sys
out=pathlib.Path(sys.argv[1]);log=(out/'container.log').read_text()
assert '--- PASS: TestNodeContainerLifecycle ' in log and '--- SKIP:' not in log
(out/'report.json').write_text(json.dumps({'commit':sys.argv[2],'status':'passed','scope':'node-container-supervision','traffic_drain':'separate-real-node-test','cross_arch_build_only':True})+'\n')
PY
