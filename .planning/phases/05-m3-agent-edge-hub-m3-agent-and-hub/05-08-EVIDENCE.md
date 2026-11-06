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
- Authorization 1.2.0 parsing, anchored share-reference storage and correlated
  successful issuer results are implemented incrementally below. Live negotiated
  renewal scheduling/transport and negative-result persistence are not yet wired.
  Full share activation occupancy/CAS and authenticated backend provisioning
  remain the 05-12/05-13/Phase 6 boundaries; no correlated legacy downgrade is allowed.
- Authenticated contiguous-confirmation retention (at least seven days),
  disputed/unknown retention, uncertain-age refusal and process-level renewal
  loss/restart/disk/time/revocation fault cases remain open.
- Protocol fixtures remain test-only; Phase 6 production authority and Phase 4
  performance qualifications are outside these claims. T-05-13/14 and G05-01
  remain open until the complete 05-08 runtime acceptance passes.


## Incremental approved authorization 1.2 prerequisites (not plan completion)

Source `adaa38826459e7df9b5d8c25b5bb428f7e4e2b25` adds the already approved
share/delegation/correlation prerequisites needed by 05-08. This is a necessary
cross-file extension to the plan's proposed file list, not a new wire contract,
dependency or claim that 05-12/05-13 were executed.

- `agent/store/share_refs.go`: immutable archived references, independent trusted
  source anchors and monotonic heads, bounded parent-chain validation, sibling
  capacity checks, and current-root checks for new admission. Historical valid
  authorizations retain their original revision while draining. Source roots
  enter only through `InstallShareSourceAnchor`, a trusted internal provisioning
  adapter; a received AuthorizationUpdate cannot provision its own root. Finite
  Store fixtures do not prove an authenticated production provisioning path.
- `agent/authorization.go` and `agent/control.go`: strict opt-in 1.2 parsing;
  legacy fields remain isolated. A correlated update requires the exact durable
  successful response from the selected issuer, current boot, request hash and
  original rule/source/snapshot/amount/TTL bounds. Nonempty policy transitions
  remain fail closed pending their runtime slice.
- `agent/store/issuer.go`: full 1.2 share/delegation binding and exact successful
  RenewalResult bytes commit in the same FULL transaction as the budget debit.
  Every new debit checks the current anchored chain again. Duplicate successful
  requests replay exact persisted bytes; changed request/scope is rejected.
- Schema 15 is additive. The migration test recreates the prior column/table
  layout, reopens it and checks that a legacy issued payload remains identical
  and neither refills nor repeats its debit. This is not a binary rollback or
  power-loss proof of the entire migration sequence.

`make phase5-check` passed on this SHA in local pinned Go 1.27.3 Linux arm64
Docker and [Actions 42161832491](https://github.com/VortexWang/Vortex/actions/runs/42161832491)
Linux amd64. Both have 30 named share/renewal race passes (10 tests repeated
three times), including anchored-root refusal, changed root fencing, invalid
chains/sibling capacity, concurrent finite exhaustion, injected write rollback,
reopen/replay, legacy migration, strict JSON and matching durable request scope.
These are component/store tests, not the outstanding live TCP/UDP renewal tracer.
The full existing gate also retained its process, scheduler, journal, Engine and
real full-disk recovery checks. Both raw recovery sets were independently
re-audited in pinned Linux: five snapshots and ten faults passed each.

Raw evidence, pair metadata and SHA-256 manifest:
`artifacts/phase5/check/adaa38826459e7df9b5d8c25b5bb428f7e4e2b25/`.
Native root tests, Agent/Hub race tests, Engine tests and vet also passed; the
required Linux disk checks come from Docker/Actions, not macOS.

Open implementation: durable negative issuer results; negotiated live request/
result handling; low-water/TTL scheduling; real TCP/UDP continuation and fault
tracer; authenticated contiguous confirmation and >=7-day safe retention.
After signer-epoch transfer, immutable old share scope currently refuses new
1.2 issuance until successor provisioning is implemented; it never strips the
share scope to obtain a legacy grant. T-05-13/14 and G05-01 remain open.

### Cross-workflow findings while validating this increment

Phase 1 run 42161832400 failed because the workflow lacked both complete Git
history and protoc, as well as the previously documented VLink review-hash
mismatch. Commit `5b0b15bf04ebde99777f39b751c4d83adc895e87` adds full checkout
history and the already locked protoc 36.2. Run 42162182135 then passed all
27 Python tests and every other listed Phase 1 check except the known VLink
contract hash. The current VLink document hash is
`bf118a61789d37854f7c18dc5676d62bac4b845040944812418419cdacb17a9e`;
its recorded approval is still `dbf1607d306720a73053848fff1ca92e1d66716957a0410f83c7c1552a112b52`.
05-05 explicitly excluded that drift from its control approval. No approval hash,
wire document or frozen-contract check was weakened to turn it green.

Phase 3 run 42162182082 passed Engine race/fuzz and Docker VLink tests, then
failed in its benchmark script. The raw benchmark exits PASS but interleaved
link diagnostics split metric lines; the script's metric extraction cannot find
all requested values. Its raw output remains diagnostic, not qualified. This
historical benchmark path is separate from Phase 5's passing accounting and
runtime checks; no full-repository CI pass is claimed.

Latest validation-only commit `5b0b15bf04ebde99777f39b751c4d83adc895e87` also
passed the full local Docker gate and [Actions 42162182113](https://github.com/VortexWang/Vortex/actions/runs/42162182113).
Both raw recovery sets were independently re-audited (five snapshots/ten faults),
and both have 30 required share/renewal race passes. Evidence, pair manifest and
cross-workflow failure excerpts are retained under its SHA directory. The earlier
`adaa388` pair remains retained separately; no result is attributed to another SHA.

## Increment: live finite renewal — 36d244b (partial, not plan completion)

Source `36d244b7747dd3e16089e4fd76ee56ad66bbddbd` passed the full
`make phase5-check` in clean pinned Go 1.27.3 Linux arm64 Docker and
[Actions 42165803700](https://github.com/VortexWang/Vortex/actions/runs/42165803700)
Linux amd64. Raw logs, five recovery SQLite snapshots per environment,
independent re-audits, pair metadata and hashes are retained at
`artifacts/phase5/check/36d244b7747dd3e16089e4fd76ee56ad66bbddbd/`.
Both environments have 54 named share/renewal race passes (18 tests repeated
three times), two required actual-binary renewal scenarios, and independent
recovery audits passing five snapshots/ten fault cases. Missing or skipped
required process scenarios fail the gate.

| Behavior | Implementation / evidence | Boundary |
|---|---|---|
| Low-water/TTL trigger | `Store.DueRenewals`, `PersistTriggeredRenewal`, `ControlClient.queueDueRenewals` | Current boot, installed rule, existing finite delegation, trusted time and separately provisioned share roots only; bounded scans and one unresolved lineage |
| Current authenticated transport | `sendPendingRenewals`, `acceptRenewalResult`, `Issuer.Renew` | Negotiated renewal-v1 on the existing control 1.3 stream; session fencing; exact durable ID/hash replay; no issuer transfer on reconnect |
| Durable terminal result | `Store.RenewHubAuthorization` | Successful debit, issue and response commit together; refusals commit without authority; changed requests conflict and legacy issuance cannot bypass a terminal result |
| Atomic local schema upgrade | `migrateRenewalAuthority`, schema 16 | FULL transaction adds renewal outcome/trigger and anchored-share schema; injected marker failure rolls back tables/columns and retains original funds; reopen preserves legacy results |
| Actual TCP/UDP continuation | `TestSchedulerProcessRenewal/low-water-tcp-udp` | Initial 8192 bytes; total issued 16384; committed 14336; remaining 2048; unknown/reserved zero at audit; Hub finite 65536 = remaining 49152 + issued 16384 |
| Lost response | `TestSchedulerProcessRenewal/lost-result` | First committed renewal response deliberately dropped; same request replay yields exactly two total issues (initial plus one renewal), with the same source conservation |

Renewal is opt-in through the already approved runtime lifecycle renewal knobs.
When enabled, omitted companion values default to 30 percent / one-second retry;
status-only configurations retain their existing negotiation. The protocol backend
fixture explicitly seeds finite source anchors before launching the real node and
test Hub. It is not production backend provisioning, a performance qualification,
or evidence that a node may create funding from a received template.

An initial repeated process run exposed a short denial after installation:
clock quantization could project below the just-authenticated issue lower bound,
and installation was marked before the clock observation. `AuthorityClock.project`
now retains the authenticated lower bound while continuity holds, and the renewal
journal is marked installed only after validated authority and clock observation.
`TestAuthorityClockNeverRetreatsBelowAuthenticatedIssue` covers zero/short elapsed
intervals; time expiry and contradiction checks remain enforced. After this fix,
the two renewal scenarios and all five regional process scenarios passed ten
consecutive native repetitions. Native development logs are explicitly marked
precommit/dirty-checkout evidence and do not substitute for the clean Linux pair.
Root/Engine tests, Agent/Hub race and vet also passed.

Still open for 05-08: renewal-specific SIGKILL/restart, revocation/old-session and
disk/time failure combinations on live traffic; authenticated contiguous
confirmation and >=7-day settled-record cleanup with indefinite dispute/unknown
retention. Existing generic recovery fault passes do not establish those new
renewal combinations. No SUMMARY, G05-01 closure or phase completion is claimed.

Other workflows on this SHA remain separate: Phase 2 passed; Phase 1 run
42165803746 failed only the existing VLink review hash (other listed checks passed),
and Phase 3 run 42165803707 failed benchmark metric extraction despite raw Go
benchmark PASS. Current excerpts are retained under `cross-workflow/`; neither
historical problem was hidden or approval hashes changed in this increment.

## Increment: crash recovery and WAL boundaries — e4aa4c4 (partial)

Source `e4aa4c47536c8958e07b0607bf63ce8bfee821d4` passed the full clean-source
Go 1.27.3 Linux arm64 Docker gate and
[Actions 42169636817](https://github.com/VortexWang/Vortex/actions/runs/42169636817)
Linux amd64 gate. Both environments pass all six actual-binary renewal process
scenarios and 63 named share/renewal/event/reader race passes (21 tests repeated
three times). Both recovery sets were independently re-read in pinned Linux:
five SQLite snapshots and ten existing recovery fault cases passed per set.
Raw logs, snapshots, audit results, pair metadata and SHA-256 manifests are at
`artifacts/phase5/check/e4aa4c47536c8958e07b0607bf63ce8bfee821d4/`.

| New required scenario | Observed result / exact scope |
|---|---|
| `hub-restart` | Test Hub commits renewal then holds replies; SIGKILL and restart replay the original ID/hash. Same TCP stream and later UDP continue; initial+renewal remain exactly two issues and 16384 issued bytes. |
| `node-restart` | SIGKILL real Agent with original renewal pending and a durable 512-byte unresolved write fixture. New boot fences all old authority; original pending request/hash and issuer's 16384-byte debit remain. Agent source: initial 8192 = remaining 1536 + committed 6144 + unknown 512, reserved zero. Hub stays offline so this cannot create a new per-boot test delegation. |
| `quota-exhausted` | Finite 8192-byte delegation yields durable `quota_exhausted`; existing TCP consumes only its remaining valid 2048 bytes. Next write receives RST; source remains initial=committed=8192, remaining zero. |
| `write-failure` | Test-only SQLite trigger rejects reservation insertion on a real active TCP path. Next write receives RST, committed stays 6144 and no uncommitted reservation is treated as authority. This is injected storage failure, not a new physical ENOSPC proof. |

The restart tracer found an actual reconnect defect: duplicate config-result
events raised a uniqueness error and prevented renewal replay. `AppendEvent`
now accepts only an exact owner/kind/payload replay; changed content returns
`ErrEventConflict`. `TestDurableEventExactReplayAndConflictAfterReopen` checks
reopen, concurrent exact replay, changed kind/payload and write failure.

Earlier local Docker attempts on `a983ab9` and `e9f2f3d` failed with SQLITE_BUSY;
their raw failed reports/root logs remain separately retained and explicitly
failed. A connection-local observer wait alone did not fix the stale WAL snapshot.
The final fix separates `Store.OpenReader` from startup/migration writes and uses
IMMEDIATE transactions for write operations before reading authority. Read-only
transactions remain deferred; busy waits stay bounded at five seconds. Regression
tests prove a reader cannot create/migrate/mutate a ledger, handles escaped file
paths, does not block a writer, and a second writer cannot overtake the first
authority snapshot. Test-only debt injection remains explicitly a storage adapter
fixture; production Agent/Hub files still each have their owning writer.

Native root tests, Agent/Hub race, vet and three complete process repetitions
passed after the final WAL fix. Earlier native fault/debt logs are marked with
their precommit scope and do not replace the final clean Linux pair. Earlier
Actions runs 42168061568/42168909697 passed, but their matching local Docker runs
failed; they are not represented as paired success.

05-08 remains executing. Still open: renewal-specific revocation/late-old-session
and trusted-time/expiry combinations, and >=7-day settled usage/receipt cleanup
under authenticated contiguous confirmation. Existing triggers already journal
known/unknown usage; what is missing is the safe age/checkpoint/dispute/idempotency
cleanup integration, not creation of usage journal entries. Unknown debt, issues,
handoff evidence and disputes must survive cleanup. No plan SUMMARY or G05-01
closure is created by this increment. All Hub tests remain protocol-backend
fixtures, not Phase 6 production authority or Phase 4 performance qualification.
