# Native Host Qualification

This procedure prepares an Ubuntu host for Phase 4 evidence. The current
project baseline requires at least 4 logical CPUs, 8 GiB memory, Ubuntu 24.04,
the pinned toolchain, and a non-loopback interface. Virtualization, shared
resources, and provider attestations are recorded when available but do not
block acceptance by themselves.

## Install the toolchain

The preflight script installs the Ubuntu packages and Go toolchain by default.
The manual commands below are equivalent and are useful when provisioning is
performed by an image builder.

```bash
apt-get update
apt-get install -y ca-certificates curl git make python3 iproute2 procps util-linux coreutils build-essential
curl -fLO https://go.dev/dl/go1.27.3.linux-amd64.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.27.3.linux-amd64.tar.gz
export PATH=/usr/local/go/bin:/root/go/bin:$PATH
go version
```

Use the `arm64` archive on an arm64 host. The expected toolchain is Go
`1.27.3`.

## Optionally record provider evidence

Create an attestation file supplied by the host provider. Do not invent these
fields from inside the guest:

```json
{
  "provider": "example-provider",
  "host_id": "provider-dedicated-host-id",
  "dedicated_resources": true,
  "dedicated_link": true,
  "issued_at": "2026-11-05T00:00:00Z"
}
```

## Run the fail-closed preflight

```bash
cd /path/to/Vortex
tools/phase4/native-host-preflight.sh \
  --interface ens17 \
  --attestation-file /etc/vortex/host-attestation.json \
  --output artifacts/phase4/native-host-preflight.json
```

`make phase4-native-preflight` passes `--install` automatically. Use
`--no-install` only for an already provisioned host or an audit-only run.

The command must exit zero before the qualification sequence. A host below the
CPU/memory minimum, unsupported OS/architecture, loopback-only topology,
missing Go version, or missing Linux tools exits nonzero and writes the reason
list. Virtualization and absent provider attestation remain informational.

## Run the qualification sequence

Only after preflight passes, use one clean SHA and preserve all raw evidence:

```bash
make phase4-d04-scan
BACKEND=native make phase4-runtime-mixed
BACKEND=native make phase4-runtime-comparison
make phase4-qualification-gate \
  RUNTIME_REPORT=artifacts/phase4/qualification/native/report.json \
  TLS_REPORT=artifacts/phase4/comparison/native/comparison.json \
  HOST_REPORT=artifacts/phase4/qualification/native/host.json \
  QUALIFICATION_TRIALS=3 \
  QUALIFICATION_WARMUP_SECONDS=60
```

The existing short workload remains diagnostic. Formal NFR qualification still
requires the runtime-authority workload, 60-second warmup, three 300-second
trials, independent TLS comparison, complete accounting evidence and the same
clean commit across all artifacts.
