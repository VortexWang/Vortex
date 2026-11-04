#!/usr/bin/env bash
set -euo pipefail

OUT="artifacts/phase4/native-host-preflight.json"
IFACE=""
ATTESTATION=""
MIN_CPU=4
MIN_MEMORY_GIB=8
# Virtualization and shared resources are recorded evidence, not acceptance
# blockers. The current project baseline requires at least 4 CPUs and 8 GiB.
REQUIRE_NATIVE=0
INSTALL=1

usage() {
  cat <<'EOF'
Usage: native-host-preflight.sh [options]

  --output PATH                 Evidence JSON path
  --interface NAME              Dedicated qualification interface
  --attestation-file PATH       Provider-signed dedicated resource/link JSON
  --min-cpu N                   Minimum logical CPUs (default: 4)
  --min-memory-gib N            Minimum memory in GiB (default: 8)
  --allow-virtualized           Record virtualization (default)
  --no-install                  Do not install missing packages/toolchain
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) OUT=$2; shift 2 ;;
    --interface) IFACE=$2; shift 2 ;;
    --attestation-file) ATTESTATION=$2; shift 2 ;;
    --min-cpu) MIN_CPU=$2; shift 2 ;;
    --min-memory-gib) MIN_MEMORY_GIB=$2; shift 2 ;;
    --allow-virtualized) REQUIRE_NATIVE=0; shift ;;
    --install) INSTALL=1; shift ;;
    --no-install) INSTALL=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

mkdir -p "$(dirname "$OUT")"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

os_id="unknown"; os_version="unknown"
if [[ -r /etc/os-release ]]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  os_id=${ID:-unknown}
  os_version=${VERSION_ID:-unknown}
fi

if (( INSTALL == 1 )) && [[ "$EUID" -eq 0 && "$os_id" == "ubuntu" ]]; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl git make python3 iproute2 procps util-linux coreutils build-essential
fi

arch=$(uname -m)
virt=$(systemd-detect-virt --vm 2>/dev/null || true)
[[ "$virt" == "none" ]] && virt=""
cpu_count=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 0)
memory_kib=$(awk '/MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)
memory_gib=$((memory_kib / 1024 / 1024))

default_iface=""
if command -v ip >/dev/null 2>&1; then
  default_iface=$(ip route show default 2>/dev/null | awk 'NR==1 {print $5}' || true)
fi
if [[ -z "$IFACE" ]]; then IFACE=$default_iface; fi
iface_exists=0
[[ -n "$IFACE" && -d "/sys/class/net/$IFACE" && "$IFACE" != "lo" ]] && iface_exists=1

commands=(go make python3 ip ss tc taskset sha256sum git)

if (( INSTALL == 1 )) && [[ "$EUID" -eq 0 && "$os_id" == "ubuntu" ]]; then
  go_arch=""
  case "$arch" in
    x86_64) go_arch=amd64 ;;
    aarch64) go_arch=arm64 ;;
  esac
  go_version_check=""
  if command -v go >/dev/null 2>&1; then
    go_version_check=$(go version 2>/dev/null || true)
  fi
  if [[ -n "$go_arch" ]] && { ! command -v go >/dev/null 2>&1 || [[ "$go_version_check" != *"go1.27.3"* ]]; }; then
    curl -fsSLo /tmp/go1.27.3.linux-${go_arch}.tar.gz "https://go.dev/dl/go1.27.3.linux-${go_arch}.tar.gz"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go1.27.3.linux-${go_arch}.tar.gz
  fi
  export PATH=/usr/local/go/bin:/root/go/bin:$PATH
fi

missing=()
for command_name in "${commands[@]}"; do
  command -v "$command_name" >/dev/null 2>&1 || missing+=("$command_name")
done
go_version=""
if command -v go >/dev/null 2>&1; then go_version=$(go version 2>/dev/null || true); fi
missing_json='[]'
if ((${#missing[@]} > 0)); then
  missing_json=$(printf '%s\n' "${missing[@]}" | python3 -c 'import json,sys; print(json.dumps([x.strip() for x in sys.stdin if x.strip()]))')
fi

attestation_valid=0
if [[ -n "$ATTESTATION" && -s "$ATTESTATION" ]] && command -v python3 >/dev/null 2>&1; then
  if python3 - "$ATTESTATION" <<'PY'
import json, sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
required = ("provider", "host_id", "dedicated_resources", "dedicated_link", "issued_at")
assert all(data.get(key) for key in required)
assert data["dedicated_resources"] is True
assert data["dedicated_link"] is True
PY
  then attestation_valid=1; fi
fi

native_ok=1
if [[ "$REQUIRE_NATIVE" == 1 && -n "$virt" ]]; then native_ok=0; fi
if [[ "$os_id" != ubuntu || "$os_version" != 24.04 ]]; then native_ok=0; fi
if [[ "$arch" != x86_64 && "$arch" != aarch64 ]]; then native_ok=0; fi
if (( cpu_count < MIN_CPU )); then native_ok=0; fi
if (( memory_gib < MIN_MEMORY_GIB )); then native_ok=0; fi
if (( iface_exists != 1 )); then native_ok=0; fi
if ((${#missing[@]} > 0)); then native_ok=0; fi
if command -v go >/dev/null 2>&1 && [[ "$go_version" != *"go1.27.3"* ]]; then native_ok=0; fi

python3 - "$OUT" <<PY
import json, os, platform, sys
out = sys.argv[1]
data = {
  "schema": 1,
  "status": "pass" if $native_ok else "not-qualified",
  "qualification_scope": "formal native host preflight",
  "os": {"id": "$os_id", "version": "$os_version", "arch": "$arch"},
  "virtualization": "$virt",
  "cpu_count": $cpu_count,
  "memory_gib": $memory_gib,
  "minimums": {"cpu_count": $MIN_CPU, "memory_gib": $MIN_MEMORY_GIB},
  "interface": {"name": "$IFACE", "exists": bool($iface_exists)},
  "go_version": "$go_version",
  "missing_commands": $missing_json,
  "provider_attestation": {"path": "$ATTESTATION", "valid": bool($attestation_valid)},
  "reasons": [],
}
if data["virtualization"] and $REQUIRE_NATIVE:
  data["reasons"].append("virtualization detected; native host proof is required")
if data["os"]["id"] != "ubuntu" or data["os"]["version"] != "24.04":
  data["reasons"].append("Ubuntu 24.04 is required")
if data["cpu_count"] < $MIN_CPU: data["reasons"].append("insufficient logical CPUs")
if data["memory_gib"] < $MIN_MEMORY_GIB: data["reasons"].append("insufficient memory")
if not data["interface"]["exists"]: data["reasons"].append("non-loopback interface is missing")
if data["missing_commands"]: data["reasons"].append("required host commands are missing")
if data["go_version"] and "go1.27.3" not in data["go_version"]: data["reasons"].append("Go 1.27.3 is required")
open(out, "w", encoding="utf-8").write(json.dumps(data, indent=2, sort_keys=True) + "\n")
print(json.dumps(data, sort_keys=True))
PY

if [[ "$native_ok" != 1 ]]; then exit 1; fi
