# P4-Q2/Q3 current coverage evidence (2026-11-06)

The current evidence baseline is clean commit
`fdaea8fd49a7b9e8022ae93e509bd309d025f8e7`.

## P4-Q2 finite integrity and fault matrix

The Docker comparison output is `/tmp/vortex-phase4-comparison-fdaea8f`.
It records:

- all 16 configured-route forged OPEN cases (eight TCP and eight UDP), with
  stable rejection codes and nonzero 32-hex audit correlations;
- UDP link-loss termination;
- direct source-to-relay-2 and source-to-destination bypass rejection;
- deterministic 64 MiB binary payload integrity;
- relay-2 termination with established TCP termination and no UDP/new-TCP
  bypass; and
- the same five-process route and clean semantic manifest as the runtime
  workload.

The Docker recovery output is `/tmp/vortex-phase4-recovery-ae858f5-docker`.
Its eight cases pass: unlisted/stale reload rejection, failed prewarm keeping
the primary route, downstream-first generation switch, old TCP half-close,
stale old OPEN rejection, primary middle failure, active middle failure with
no fallback, and authorized spare restart. Both outputs explicitly scope
themselves to finite tracer behavior and exclude durable activation/ledger
recovery.

This closes the enumerated current-configuration Q2 evidence set. It does not
claim universal attack coverage, identity-handshake coverage beyond the Go
identity tests, or availability failover for an unconfigured alternate path.

## P4-Q3 Engine authority boundaries

The current Docker snapshots output is `/tmp/vortex-phase4-snapshots-fdaea8f-docker`.
Existing TCP stayed on immutable `old` snapshot usage after selection revision
2, a new TCP session used `new`, and both snapshot ledgers settled with no
sessions or held bytes. The path stayed on the startup-pinned route.

The current Docker source-credit outputs are:

- `/tmp/vortex-phase4-credit-fdaea8f-docker` (TCP): eight in-budget 16 KiB
  exchanges, then existing/new flow denial, exact 262,144-byte use and zero
  held/unknown/session leakage;
- `/tmp/vortex-phase4-credit-fdaea8f-docker-udp` (UDP): 64 in-budget 1 KiB
  datagrams with four interleaved TCP exchanges, then existing/new UDP and
  new TCP denial, exact 262,144-byte use and zero held/unknown/session leakage.

The targeted race suite also passes for policy windows, share CAS/drain,
immutable snapshot selection, source credit, unknown outcomes, delayed
accepted/rejected/unknown issuer results and route generation fencing:

```text
go test -race ./vlink ./runtime/accounting ./runtime/link ./runtime/relay ./cmd/vortex-relay
```

The remaining boundary is explicit: policy and ShareGate are injectable Engine
adapters rather than a production Agent/Hub authority, the issuer is
process-local, and no SQLite/PostgreSQL receipt or balance is claimed. Q3 is
therefore finite Engine coverage, not full NFR-039/NFR-040/NFR-044 acceptance.

