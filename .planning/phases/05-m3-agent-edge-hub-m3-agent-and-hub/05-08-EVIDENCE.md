---
phase: 05
plan: '08'
status: verified
plan_complete: true
updated: 2026-11-06
---

# 05-08 incremental runtime evidence

Final plan status: verified on source `0c104a600300b9f5923778a893013604bbb03a80`.
Both tasks now have same-SHA Docker/Actions evidence and independent raw-ledger
audits. Earlier incremental entries below retain their historical partial status;
they are not rewritten as prior completion. Shared requirement IDs remain pending
until all declaring plans complete. Phase 5 is still executing.

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

## Increment: conservative settled-usage compaction (2026-11-06)

Source `b0ab02ad8c4e93ee8421a2983fa2f9a5f3fa6587` implements the retention
portion of task 2 without changing the approved wire/runtime contracts or
adding dependencies. `Store.migrateUsageRetention` is a separate additive FULL
transaction and migration marker; failure rolls back its tables and marker,
leaving all prior authority and usage intact. The existing renewal schema
version remains 16. Reopen and injected migration/deletion failures are covered.

`Store.PruneSettledUsage` requires Hub ownership, bounded work (1..1000),
trusted conservative time, an exact backend usage receipt, an exact confirmed
known-usage journal entry, and a contiguous ledger checkpoint with no earlier
unconfirmed hole. Only observing all confirmations with a trusted upper time
bound starts retention; historic wall-clock timestamps do not count. A later
trusted lower bound must reach that upper bound plus seven days. Out-of-order
ACKs, absent backend receipts, untrusted time, explicit disputes and recorded
unknown-identity conflicts cannot authorize deletion. The bounded scan prioritizes
mature records, then unaged records, to avoid starvation behind young records.

Compaction atomically removes duplicate `usage_batches` payloads and transport
receipts, replacing them with permanent node/batch/snapshot/hash/sequence
`settled_usage_tombstones`. The original confirmed `hub_ledger` payload remains
available for audit and duplicate ledger ACKs, preserving the sequence high-water
mark. Replay through `PersistUsageLedger`, `BackendUsageConfirmed` and
`ConfirmBackendUsage` recognizes tombstones without reinsertion or another
backend acceptance. Changed known payloads are rejected and retained in
`usage_disputes`; an unknown record cannot reuse a compacted known identity.
Issues, handoffs, source balances, unknown liabilities and disputes are never
removed. This compacts duplicate transport storage; it does not archive or
prune the authoritative financial journal.

`Issuer.ReplayLedger` invokes bounded cleanup after durable replay when a valid
trusted clock is available. Missing/untrusted time defers cleanup. Agent stores
cannot invoke it: their Hub ACK is not authenticated backend settlement. A
production backend transport and Hub service scheduling are not claimed by the
protocol-adapter tests and remain in their respective later plans.

Five named `TestSettledUsageRetention*` regressions cover seven-day lower/upper
bounds, restart replay, checkpoint holes, missing backend confirmation, disputes,
unknown debt, FULL migration/deletion rollback, bounded scan progress, and
concurrent prune/replay/receipt confirmation. The Hub integration test
`TestRecoveryRetainsReceiptIdentityAfterUsageCompaction` exercises replay and
cleanup against separate durable Hub/backend SQLite files, including untrusted
time and post-cleanup idempotency. All six run three times under race in the
required gate, with explicit named PASS/SKIP checks.

`audit_usage_retention.py` independently opens the retained SQLite snapshot,
verifies integrity and every surviving payload hash, confirms the exact known
hash tombstone/checkpoint, and checks disputed, unconfirmed and unresolved
unknown records remain. Its clock is a deterministic conservative-bound fixture;
it does not represent seven days of production observation. Supporting native
root/race/vet and targeted repetitions passed. Native macOS Python SQLite 3.51.0
failed to open the raw snapshot and is not a passing audit; the pinned Linux
container audit passed. Final same-SHA Docker/Actions evidence follows below.

05-08 remains executing: renewal-specific revocation, late old-session replies
and stale/exact-expiry trusted-time combinations still need their own evidence.
No SUMMARY, plan completion or Phase 6 production-authority claim is made.

Paired full-gate result: source `b0ab02ad8c4e93ee8421a2983fa2f9a5f3fa6587`
passed clean-clone pinned Go 1.27.3 Linux arm64 Docker and GitHub Actions
Linux amd64 [run 42171635825](https://github.com/VortexWang/Vortex/actions/runs/42171635825).
Both executed `make phase5-check`, the six live renewal process cases and all
six new retention tests three times under race (18 named passes per host).
Each retained its own compressed SQLite snapshot, hashes, raw logs, recovery
fixtures and passing independent retention/recovery audit results. Downloaded
Actions evidence was independently re-read again in the pinned Linux container;
18 already-committed historical SHA evidence directories from the uploaded
artifact were omitted from this new run's local evidence, preserving all its
current top-level output. Raw current evidence, run metadata, `pair.json` and
`files.sha256` are under `artifacts/phase5/check/b0ab02ad8c4e93ee8421a2983fa2f9a5f3fa6587/`.
Supporting native logs are explicitly precommit scope. The macOS Python audit
failure is recorded as not passed and is not substituted for Linux results.

## Increment: renewal revocation, stale-session and absolute-time safety (2026-11-06)

Source `0c104a600300b9f5923778a893013604bbb03a80` adds safety evidence at the
actual existing boundaries, without changing product behavior, dependencies,
approved contracts or time/authorization policy.

- `TestRenewalSafetyRevocationDominatesDelayedAndConcurrentSuccess` uses finite
  authenticated Hub issuance, the real Agent renewal receiver, FULL SQLite
  transactions and the Engine's `runtimeAuthorityGate`. Revocation before
  success, after success, and concurrent with success all deny new OPEN and
  existing-session reservation. The source-owner check correctly rejects a
  late grant when its owner was already revoked: the original durable success
  remains `received` and Agent initial authority remains 4096 while Hub issued
  authority stays 8192. When installation wins first, Agent initial authority
  is 8192 and later revocation makes all records unusable. Both outcomes retain
  committed 64 and unknown 128 bytes, with no synthetic release or consumption.
  Exact success replay cannot resurrect revoked authority. Initial test
  expectations incorrectly required installation in both orders; expectations
  were corrected to assert the existing serializable outcomes and balances,
  rather than changing the safe implementation.
- `TestRenewalSafetyLateOldMutualTLSResultCannotInstall` observes the original
  renewal request on a real mTLS stream, starts a successor connection to the
  same authenticated issuer, then attempts delivery of a fully valid original
  success on the old stream. The old receiver joins; the immutable request
  remains pending, no new grant installs, authority-clock anchor is unchanged,
  and Hub's unseen issuance remains reserved. The real current-session mutex
  and cancellation path perform the rejection. No fake response parser or
  test-only product bypass is used.
- `TestRenewalSafetyTimeBoundaryCannotReleaseOrConsume` installs a valid renewal
  and checks both a trusted adapter at the exact exclusive expiry boundary and
  the real boot-local AuthorityClock after its maximum anchor age. New and
  existing Engine reservations fail; committed 64/unknown 128 and conserved
  balances remain unchanged. The exact-boundary adapter is a deterministic
  component fixture, not a claim of nanosecond scheduling in a live process.
- `TestSchedulerProcessRenewal/expiry-with-pending-result` runs the shipped
  vortex-node/test-hub binaries and real VLink TCP against a ten-second hard
  delegation deadline. The Hub commits renewal but intentionally withholds the
  reply. After the real absolute deadline, the existing TCP stream explicitly
  returns RST. Agent remains at initial 8192, consumed 6144, remaining 2048,
  unknown/reserved zero; the same request/hash remains pending. Hub retains
  issued 16384 and remaining 49152. Deadline and renewal cannot free either
  unseen issuance or the original residual credit. This complements the prior
  six process scenarios and the deterministic exact-boundary assertion.

`audit_renewal_safety.py` independently reopens twelve SQLite snapshots from the
six finite safety fixtures. It reconstructs the approved domain-separated
request hash by removing protobuf field 13, compares original Agent/Hub request
and response bytes/hashes, checks receipt states and all retained payload hashes,
and recomputes source conservation and unknown-liability retention. No Go store
function is imported into the auditor. Each named safety case must pass three
times under race, with twelve snapshots and an independent passing audit. The
full gate also requires the seventh named actual-process expiry case; SKIP or
missing case remains failure. Native root/race/vet, three safety repetitions and
the real expiry process case passed before commit; same-source Linux results
are recorded below when complete. No broad Phase 5 completion is inferred.


## Final plan gate and acceptance audit

Source `0c104a600300b9f5923778a893013604bbb03a80` passed clean-clone Linux
arm64 Docker and [Actions42175493658](https://github.com/VortexWang/Vortex/actions/runs/42175493658)
Linux amd64 with the same `make phase5-check`. Both ran the seven actual-process
renewal cases, six new safety cases three times under race, six retention cases
three times under race, scheduler/probe and share/fencing/journal race suites,
Engine race and actual full-disk/recovery regressions. Required contract checks
used pinned generators. Independent audits re-read twelve safety snapshots,
the settled/disputed/unknown cleanup snapshot and the five recovery snapshots
with ten existing fault cases in each environment. All passed. Raw logs, SQLite,
manifests, run metadata, selected current artifact contents, `pair.json` and
`files.sha256` are retained under `artifacts/phase5/check/0c104a600300b9f5923778a893013604bbb03a80/`.
The downloaded Actions artifact also contained 19 historical SHA directories;
only those redundant historical copies were omitted from this new evidence set.

| Plan assertion | Current authoritative evidence | Result and boundary |
|---|---|---|
| Actual target probes, health/version reports, configured bounds | NodeScheduler/CurrentNodeReport tests and required threefold race gate | Passed; observational snapshots never fund or refresh trusted time |
| Finite low-water remote renewal over current stream, TCP/UDP continuity | Seven `TestSchedulerProcessRenewal` process scenarios | Passed; fixture roots are explicitly finite, not production backend provisioning |
| Durable ID/hash, lost result/crash replay, no repeated issue | Renewal journal/Hub issue/source audits, real Hub and node SIGKILL scenarios | Passed; unseen issuance stays reserved, old boot authority is settlement-only |
| Source exhaustion and unrecordable writes fail closed | Actual exhausted/write-failure process cases and full-disk runtime recovery suite | Passed; injected trigger is labeled separately from real SQLITE_FULL evidence |
| Old-session, revocation, stale clock, exact and real expiry | Six named repeated safety cases, real mTLS successor, seventh process expiry | Passed; deterministic exact-time adapter and actual process deadline are separately scoped |
| Bounded telemetry and financial retention isolation | Latest-state cache/probe tests; Hub conservative retention/audits | Passed; only duplicate settled transport payload/receipt storage compacts; original journal/tombstones, disputed and unknown debt survive; Agent Hub ACK alone cannot prune |
| No synthetic unknown release on timeout/reconnect/crash | Source audits plus twelve safety ledger snapshots | Passed within finite tested scenarios; no unbounded model or full-product acceptance claim |

Both task done criteria are satisfied at the planned runtime/protocol-adapter
boundary. No new normative contract or dependency was introduced. Added local
Store migrations, reader isolation, source anchors and independent audit helpers
were necessary to make the runtime acceptance reviewable. 05-08 can close;
05-09..05-14 and phase-wide UAT/verification remain outstanding.
