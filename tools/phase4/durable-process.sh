#!/bin/sh
# Independent Node and protocol-test Hub, not a full D-04/NFR verdict.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
out=${VORTEX_PROCESS_EVIDENCE_DIR:?set a fresh evidence directory}
[ ! -e "$out" ] || { echo 'refusing to overwrite process evidence' >&2; exit 1; }
mkdir -p "$out"
out=$(CDPATH= cd -- "$out" && pwd)
finish() {
 code=$?
 trap - EXIT
 python3 - "$out" "$code" <<'REPORT'
import hashlib, json, pathlib, sys
out=pathlib.Path(sys.argv[1])
log=(out/"test.log").read_text() if (out/"test.log").exists() else ""
required=["TestDurableAuthorityProcessesTCP","TestDurableAuthorityProcessesUDP","TestDurableAuthorityProcessesConcurrentTCP","TestDurableAuthorityProcessesConcurrentRenewal"]
verified=[name for name in required if f"--- PASS: {name} (" in log]
passed=sys.argv[2]=="0" and verified==required
report={"schema":1,"status":"pass" if passed else "fail", "verified_tests":verified,
        "commit":(out/"sha").read_text().strip() if (out/"sha").exists() else None,
        "dirty":bool((out/"status").read_text()) if (out/"status").exists() else None,
        "scope":"independent-node-and-test-hub-tcp-udp-functional",
        "backend":"local-protocol-fixture", "performance_qualified":False,
        "certificate_renewal_exercised":False,
        "binary_sha256":{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in (out/"bin").glob("*") if p.is_file()}}
(out/"report.json").write_text(json.dumps(report,indent=2)+"\n")
if not passed: sys.exit(1)
REPORT
 exit "$code"
}
trap finish EXIT
export VORTEX_PROCESS_EVIDENCE_DIR="$out"
export VORTEX_PROCESS_BIN_DIR="$out/bin"
mkdir -p "$VORTEX_PROCESS_BIN_DIR"
git rev-parse HEAD > "$out/sha"
git status --porcelain > "$out/status"
go build -o "$VORTEX_PROCESS_BIN_DIR/vortex-node" ./cmd/vortex-node
go build -o "$VORTEX_PROCESS_BIN_DIR/vortex-test-hub" ./cmd/vortex-test-hub
go test ./agent -run '^TestDurableAuthorityProcesses(TCP|UDP|ConcurrentTCP|ConcurrentRenewal)$' -count=1 -v > "$out/test.log" 2>&1
