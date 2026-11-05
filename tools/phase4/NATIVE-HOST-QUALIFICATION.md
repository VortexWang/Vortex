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

The existing short workload remains diagnostic. Set `PHASE4_FORMAL=1` for
each runtime and comparison command to use the finite source-credit authority
instead of qualification-only mode. Use the configured event limit needed by
the 300-second runs (up to the fixture's hard cap), and preserve each trial in
its own output directory. Formal qualification requires 16 TCP streams, at
least 400 Mbps mixed TCP, at least 99 Mbps of paced UDP offered load, at most
0.1% UDP loss, a same-load runtime/TLS ratio of at least 70%, 60-second warmup,
three 300-second trials, complete accounting evidence and the same clean commit
and semantic workload across runtime and TLS artifacts.

Example invocation for each trial:

```bash
PHASE4_FORMAL=1 PHASE4_TRIAL_ID=trial-1 VORTEX_QUALIFICATION_EVENT_LIMIT=50000000 \
MIXED_DURATION_SECONDS=300 MIXED_TCP_STREAMS=16 MIXED_UDP_MBPS=100 \
BACKEND=native QUALIFICATION_DIR=artifacts/phase4/formal/trial-1 \
tools/phase4/qualification.sh
```

Repeat with unique trial IDs and output directories for trials 2 and 3. Run the
independent comparison with `PHASE4_FORMAL=1`,
`PHASE4_TRIAL_ID=comparison`, `MIXED_DURATION_SECONDS=300`, and the final
runtime directory on the same clean commit. Run a separate 60-second formal
warmup into its own directory. The gate requires the warmup report and exactly
three runtime trial reports, then checks their IDs, metrics, commit and
semantic-manifest identity; it rejects dirty evidence. A passing NFR-003/005
performance gate does not close the separate Phase 4 gaps or approve D-04.

Supply `RUNTIME_TRIAL_REPORTS="trial-1/report.json trial-2/report.json trial-3/report.json"`
and `WARMUP_REPORT=warmup/report.json` to `make phase4-qualification-gate`.
