# Clean Docker runtime and authority evidence (2026-11-06)

Commit `e5bcb03f146ff2dba0400e4a03e43efcadc8027d` is the clean SHA for this
evidence. It includes the Ed25519-capable OpenSSL selection used by the
qualification certificate generator and recovery fixture. The worktree was
clean when both runs recorded `host.json`.

## Runtime-authority mixed workload

Command shape:

```text
DOCKER_CONTEXT=orbstack BACKEND=docker PHASE4_FORMAL=1
PHASE4_TRIAL_ID=e5bcb03-runtime-diagnostic
QUALIFICATION_DIR=/tmp/vortex-phase4-runtime-clean-e5bcb03-2
MIXED_DURATION_SECONDS=5 MIXED_TCP_STREAMS=4 MIXED_UDP_MBPS=100
make phase4-runtime-mixed
```

The five-process, three-relay Docker route completed with
`runtime_correctness=pass`, `dirty=false`, and `authorization_mode=runtime-authority`.
The 5-second mixed phase measured 2,968.87 Mbps TCP, 99.95904 Mbps offered UDP,
and zero UDP loss. Source credit was conserved and settled. Destination
execution totals reconciled exactly at `4,118,038,740` bytes in both
directions, with 319,833 committed events and no classified drops.

The driver ran in the dedicated Linux client container on the OrbStack VM;
the host metadata still identifies the development host as Darwin/arm64. This
is a Docker diagnostic on a shared development VM, not the Ubuntu 24.04,
4-vCPU/8-GiB dedicated-host condition required by P4-Q1.

## Same-SHA comparison and fault matrix

The comparison was rerun with `PHASE4_FORMAL=1` so the generated route had the
same runtime-authority semantic manifest as the mixed workload. Output is
`/tmp/vortex-phase4-comparison-clean-e5bcb03-3` and records the same clean
commit and semantic manifest.

- Independent TLS mixed TCP: 4,992.85 Mbps; UDP offered 99.92064 Mbps; UDP loss 0.
- Runtime/TLS mixed TCP ratio: `0.5946236142`, below the 0.70 diagnostic threshold.
- Sixteen forged OPEN TCP/UDP cases and UDP link-loss probe: pass.
- Direct source-to-relay-2 and source-to-destination bypass probes: pass.
- Deterministic 64 MiB binary integrity check: pass.
- relay-2 termination: established TCP terminated, UDP and new TCP did not bypass; pass.
- `comparison.json`: `performance_qualification=not-qualified`.

The ratio, short duration, four-stream load, shared VM placement and absence
of repeated 300-second trials keep P4-Q1 and NFR-003/NFR-005 unqualified.
The fault matrix is finite and explicitly scoped; it does not establish
exhaustive NFR-006 coverage or failover availability.

## Remaining debts

This evidence does not lock D-04 block/target/waterline parameters. It also
does not turn the process-local source credit adapter into a persistent Agent
or Edge Hub authority, receipt ledger, or PostgreSQL-backed balance. Those
items remain under the P4-Q4 review and the Phase 5/6 handoff recorded in
`04-CLOSURE.md`.

## Regression checks on the same commit

```text
go test -race ./...                         # pass
go vet ./...                                # pass
python3 -m unittest discover -s tools/phase4 -p 'test_*.py'  # 20 pass
DOCKER_CONTEXT=orbstack make phase4-m2-check # pass
```

