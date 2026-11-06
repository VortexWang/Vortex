---
phase: 05
plan: '08'
status: incremental
plan_complete: false
updated: 2026-11-06
---

# 05-08 incremental runtime evidence

05-08 remains executing. No SUMMARY exists and no shared requirement or gap is
marked complete by these increments. The original plan's low-water TCP/UDP
remote-renewal tracer and retention/fault acceptance remain required.

## Durable request journal — 464a0c6

Source: `464a0c6852842d24943ae0c2fdebf2cd48d69eae`.

- `contracts/renewal.go`: approved domain-separated deterministic protobuf
  digest, canonical UUID/counter/TTL bounds, strict unknown-field rejection,
  response shape/correlation checks. No normative contract bytes changed.
- `agent/store/renewal_requests.go`: FULL-WAL request-before-send journal,
  immutable issuer/request identity, one unresolved intent per source lineage,
  bounded same-issuer/same-boot replay page, durable received results. Changed
  payloads/issuers conflict; a fresh boot does not clear an unresolved lineage.
- Receipt is separate from installation. `MarkRenewalInstalled` requires the
  exact already-committed authorization payload with approved 1.2.0 correlation,
  matching node/boot/issuer/path/epoch. This method records journal completion;
  it does not install authorizations, refund holds or settle unknown usage.
- Six named contract/journal tests run three times under race in the required
  gate. They cover reopen, duplicate/changed requests/results, concurrent
  lineage selection, injected request/result/completion write failure and
  receipt-without-authority. Installation storage test explicitly calls the
  trusted Store adapter directly; it is not full wire validation/Engine top-up.

Same-source `make phase5-check` passed:

- Docker Linux arm64, pinned Go 1.27.3/protoc 36.2:
  `artifacts/phase5/check/464a0c6852842d24943ae0c2fdebf2cd48d69eae/docker/`.
- [Actions 42157618782](https://github.com/VortexWang/Vortex/actions/runs/42157618782),
  Ubuntu 24.04 amd64:
  `artifacts/phase5/check/464a0c6852842d24943ae0c2fdebf2cd48d69eae/actions/`.
- Paired reports match the source SHA; each has 18 named renewal test passes.
  Independent recovery audits rerun against both raw sets passed five snapshots
  and ten faults each. These retained older recovery regressions do not prove
  new process-level renewal crash/full-disk behavior.

## Bounded probes and current-session reports — 29b3085

Source: `29b30852331798028073d5f20bab37c026541102`.

- `agent/scheduler.go:NodeScheduler` runs a fixed worker pool (candidate default
  16, maximum 16), at most 1024 configured targets and one in-flight operation
  per target. It joins workers on cancellation and retains latest snapshots.
- Probe interval/timeout/report interval consume the approved lifecycle fields
  (candidate defaults 3s/1s/10s). Three consecutive failures mark unhealthy;
  two successes recover healthy; counters and observed latency saturate.
- `SchedulerForRegion` resolves references only against the installed static
  Engine rule target. An unapplied rule or UDP without a supported health
  response reports unknown. No peer-supplied probe address is introduced.
- Actual `vortex-node` starts the scheduler for lifecycle configuration. Reports
  carry read-only applied version/current-boot session observations through the
  existing serialized ControlClient sender and monotonic stream sequence. The
  queue coalesces to one report while disconnected; probe samples do not grow
  durable events. Report timestamps never observe the authority clock.
- The protocol-test Hub validates bounded report shape, retains an ephemeral
  current-session snapshot, rejects old sessions and clears the snapshot on
  disconnect. Its test-only current-report output is atomically overwritten.
- Eight named probe/report tests run three times under race. Actual binary
  regional-process tests now also require a valid current-stream periodic
  report from each failover scenario. The TCP continuity scenario remains.

Local full root tests, targeted race tests, actual binary process tests and vet
passed. The same source passed full `make phase5-check` in Docker Linux arm64
and [Actions 42158960763](https://github.com/VortexWang/Vortex/actions/runs/42158960763)
on Ubuntu 24.04 amd64. Each has 24 named scheduler/report race passes (eight
tests, three repetitions) and 18 named renewal journal passes. Five required
regional process scenarios passed; the four failover cases additionally require
an actual periodic report (their target list is empty). The installed-target
TCP probe and unsupported UDP behavior have targeted network/component tests;
the full process TCP/UDP renewal tracer is still outstanding.

Raw reports/logs/snapshots, paired SHA/result metadata and file digests:
`artifacts/phase5/check/29b30852331798028073d5f20bab37c026541102/`.
Both sets' independent raw recovery audits were rerun using pinned Go-image
Linux Python/SQLite: passed five snapshots and ten faults each. Native macOS
was used for root/component/process/vet feedback; it is not substituted for
Linux full-disk acceptance. No check was skipped and called passed.

## Remaining acceptance and implementation prerequisites

- Actual remote RenewalRequest/Result issuance and application, configurable
  low-water/TTL trigger, minimum retry, original-boundary enforcement and live
  TCP/UDP source-conservation tracer are not complete.
- The current authorization consumer and fixture issuer still consume/produce
  legacy 1.0.0/1.1.0. Approved 1.2.0 renewal correlation additionally requires
  delegation/share references and authenticated source anchoring. Those
  interfaces overlap the planned 05-12/05-13 adapter/share work. Implement the
  necessary approved prerequisites before claiming new remote renewal; accepting
  a correlated legacy payload or stripping share restrictions is insufficient.
- Authenticated contiguous-confirmation retention (at least seven days),
  disputed/unknown retention, uncertain-age refusal and process-level renewal
  loss/restart/disk/time/revocation fault cases remain open.
- Protocol fixtures remain test-only; Phase 6 production authority and Phase 4
  performance qualifications are outside these claims. T-05-13/14 and G05-01
  remain open until the complete 05-08 runtime acceptance passes.
