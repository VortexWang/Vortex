---
phase: 04-m2-m2-multi-hop-engine
plan: 12
status: partial-slice-verified
---

# Remaining qualification work

## Completed safety fixes

Runtime commits `ea77a38` and `35c5c6b` correct two real gaps uncovered during
closure review:

- A live ShareGate session now rejects reservations at the absolute lease end,
  including expiry while the inner gate waits. It returns an unused reservation
  and retains unresolved/unknown consumption. Closing renewal ownership remains
  distinct from expiring an already valid lease.
- PolicyBoundary now changes the policy snapshot of an existing flow at T while
  retaining its original rule, execution point and path. Changing the active
  path before T does not cause the policy transition to roll it back. Pending
  batches keep their old resolvers; unknown liabilities remain in their original
  snapshot. Missing/ambiguous policy authority denies, and time rollback after
  activation cannot reopen the old policy.

The previous policy test explicitly expected post-T consumption from the old
snapshot. That assertion was incorrect and is replaced by tests for policy
transition, two paths x two policies, conservation and unknown retention.

Validation: local Engine `go test ./...`, full `go test -race ./...` (ea77a38),
targeted race checks after the rollback guard, and `go vet ./...` pass. The shared
`DOCKER_CONTEXT=orbstack make phase4-m2-check` passes on clean `35c5c6b`.
These are Engine/runtime checks, not proof of a persisted Agent/Hub ledger.

## Existing performance gate reconciled

The unchanged `fe2a4ba` raw measurements and corrected evaluator still pass
`qualification_gate.py`: three 300-second trials, 300-second warmup, finite
runtime source authority, zero UDP loss, and independent TLS ratio 0.737802.
NFR-003/005 passed on that measured build. Runtime changes in this plan require
a new same-build formal performance run before claiming the latest build qualified. The measured SHA is
not relabeled as either of the runtime safety commits above. See the complete
re-evaluation record in `../../quick/20261105-runtime-authority-gate/EVIDENCE.md`.
This is the existing single-host qualification contract; it does not add new
proof of per-node dedicated CPU/memory or physical independent link isolation.

## D-04 experiment design

Clean runtime SHA `d1ee7d2`, dedicated Ubuntu host `43.254.164.7`.
`tools/phase4/d04_run.py` runs seven actual one-factor configurations, three
repetitions each, using 50 fresh short flows plus five warmup connections, 16 TCP
streams, 15-second TCP-only and 15-second TCP+100 Mbps UDP phases. Every run
reconciles target bytes and source credit, checks zero outstanding/unknown/denial
state, and records hashes, actual throughput and raw latency samples.

The reference grant-rate input is explicitly fixed at 100 Mbps for controlled
comparison: 10/30/60 seconds mean 125/375/750 million bytes. It is not a claim
that the running >1 Gbps mixed stream has a 30-second adaptive grant. Timing
observations measure the synchronous process-local issuer only. Bounded log2
histograms report percentile bucket upper bounds, not exact quantiles. Small
issuer sample counts and scheduler/GC pauses limit p99 interpretation.

The old 27-entry generator remains a full-factorial inventory. Its incorrect
`one_factor_at_a_time=true` label is corrected; the actual seven-case executor
is the one-factor experiment.

### Completed D-04 scan results

All 21 runs completed, source credit conserved and settled, and UDP loss was
zero in every run. Median mixed TCP throughput across the three repeats:

| Changed factor | TCP Mbps | Short-flow median ms | Peak held bytes (max) |
| --- | ---: | ---: | ---: |
| Baseline: 256 KiB / 30 seconds / 30% | 1030.47 | 1.674 | 133658048 |
| Initial block 64 KiB | 1032.85 | 1.665 | 131230149 |
| Initial block 1024 KiB | 1031.93 | 1.678 | 135365144 |
| Grant reference 10 seconds | 1031.18 | 1.694 | 132925168 |
| Grant reference 60 seconds | 1033.88 | 1.688 | 132798728 |
| Waterline 20% | 1032.64 | 1.676 | 133387968 |
| Waterline 50% | 1034.22 | 1.689 | 132134456 |

The spread is small; these results do not establish a parameter winner or
justify changing a production default. The largest observed p99 issuer bucket
upper bound is 2.097151 ms, for the local allocator only. D-04 measurement of
these static candidates is complete; production lock remains open.

## TCP opening path correction

The delayed-path run on `d1ee7d2` found extra first-byte delay of 103.65 ms
(pool), 103.79 ms (mux), and 103.87 ms (auto) with 10 ms one-way netem delay.
The delayed ports were the four VLink legs and the exit target; the client
entry was not delayed. This is an out-of-profile stress diagnostic, not a
production latency qualification.

Code inspection confirmed `source.WaitOpen` and each relay's downstream
`WaitOpen` still serialized data despite the earlier low-level prefix support.
`91e409e` connects the actual TCP path: source credit is acquired before sending,
each locally authorized hop pipelines a bounded 64 KiB prefix, the receiver
consumes OPEN_OK independently, and excess early data waits within the existing
bounded windows. The explicit 64 KiB runtime pending limit replaces the prior
effective one-byte default, which also prevented useful early forwarding. UDP still waits for OPEN_OK. Batched writes now enforce the
same early-data limit, and FIN remains ordered when it precedes authorization.

A raw 100 Mbps Darwin trial failed with UDP loss/accounting mismatch and is
retained as a failed host-limited attempt. A reduced 1 Mbps native functional
trial reconciled all bytes. It is not used as Linux performance evidence.

### Current native validation — 91e409e

The dedicated machine completed all three modes with 500 microseconds of
one-way delay on each measured segment, preserving an undelayed client entry.
The five independently sampled median segment RTTs were each below 2 ms.
`latency_gate.py` recomputes medians from the raw samples and returns pass:

| Mode | First byte median ms | Sum of segment medians ms | Additional ms |
| --- | ---: | ---: | ---: |
| pool | 8.722570 | 6.601066 | 2.121505 |
| mux | 8.806085 | 6.643779 | 2.162306 |

Auto additionally measured 2.178 ms, but is not substituted for either required
mode. All three modes passed binary integrity, configured forged-OPEN and
bypass probes, and middle-hop failure checks. Explicit mode/pool-reuse assertions
in `modes.sh` passed. Each first-byte and RTT series retains 50 raw samples.
The delay harness, qdisc/filter dumps and raw JSON remain in the evidence tree.
The 10 ms stress run and 0.5 ms qualifying run have different network conditions;
their medians are not presented as a same-environment speedup comparison.

Native Ubuntu full Engine race and vet, finite TCP and UDP source-credit tests,
and immutable-snapshot runtime tests passed on `91e409e`. The same-source local
Docker shared check passed; 13 Python tests cover report gates, including missing
or inconsistent latency data and non-finite samples. No GitHub Actions run for
this new commit is claimed.

Artifacts are retained at `artifacts/phase4/d04-d1ee7d2`,
`artifacts/phase4/modes-d1ee7d2-delay`, `artifacts/phase4/modes-91e409e-delay`, and
`artifacts/phase4/closure-91e409e` on both the dedicated machine and this worktree
(compact JSON/metadata copy; large JSONL remains on the dedicated machine).
The local archive is `/tmp/phase4-closure-evidence.tar.gz`.
The host was checked after completion: both buffer maxima are back to 212992,
loopback qdisc is `noqueue`, and the remote worktree is clean.

## Remaining completion boundaries

D-04 cannot be marked production-locked solely from this sweep. Dynamic
one-minute rate estimation, cold/idle policies, delayed/lost issuer responses,
share drain occupancy, durable issuance and a final selected-combination trial
are not established by a static funded allocator. The scope is recorded rather
than labeling these observations as remote Hub measurements.

NFR-039/040/044 retain integration obligations: trusted persistent time,
pre-issue/missing-authority bounded request, persisted share ownership/CAS
receipts, restart recovery, and per-source durable snapshot accounting. These
are already assigned to Agent/Hub integration, but assignment is not a pass.

## Follow-up: adversarial protocols and pending-frame ordering

The six configured forged-OPEN cases now run for both TCP and UDP. The local
five-process test produced twelve rejected cases with expected rejection codes
and nonzero correlation IDs, plus the existing UDP link-loss check.

A fresh VLink run exposed a real race in `acceptRemote`: marking the stream open
before draining its cached prefix allowed newly received DATA to overtake that
prefix. Pending-frame enqueue now occurs under the stream-table lock before
publishing the open state. The queue operation is nonblocking. The existing
bounded-prefix/FIN-order regression passed 100 repetitions under race; the full
Engine race suite and targeted vet also passed.

Integration is still NOT qualified: both local comparison attempts stopped at
64 MiB binary transfer with a stream reset. Logs remain under
`artifacts/phase4/attack-matrix{,-fixed}/comparison/`. The second failure persists
after the ordering fix, so the ordering fix must not be claimed to resolve it.
The twelve-case forged-OPEN report passed before that failure. These local runs
used a dirty worktree and are diagnostic, not clean-SHA qualification evidence.
The latest main-worktree Phase 5 implementation has progressed to persisted
in-session renewal (86640e7), but still records pending handoff/recovery work;
its uncommitted work was left untouched.

## Persistent goal continuation: case-level attack gate

Added `attack_gate.py` to the comparison execution path. It requires every one
of twelve configured TCP/UDP rejection cases, exact rejection classes, nonzero
128-bit audit correlation IDs, and UDP link-loss termination. A top-level pass
flag no longer suffices for this enumerated matrix. Tests remove each case,
substitute wrong classes/correlations and remove the UDP half of the matrix;
all fail closed. The complete local diagnostic matrix passes. Eighteen Python
checks pass in total. This is not full identity, durable authority or recovery
coverage, and does not relabel the earlier dirty local run as formal evidence.

Live main-worktree review now finds Phase 5 commits through fef53ed (persisted
signer remainder handoff), with active uncommitted contract/store work. Durable
integration must use a coherent committed baseline; no uncommitted files from
that worktree were copied or overwritten. D-04 adaptive issuance/idle proof and
full cross-layer attack verification remain part of the active goal.

## Durable baseline integration (2026-11-05)

Integrated committed main baseline fef53edf5a383765cc5e0750a4b79a137c98e0ee
into the isolated Phase 4 branch, preserving the owner workspace changes.
Resolved the CreditGate merge to retain observation fields and durable sink/fault
state, and carry FlowKey/RuleID into every session. Propagated the source-only
awaitOpen flag through the shared session bridge; exit sessions are already
authorized and do not consume OPEN_OK. CreditBook pins the trusted RuleID on
both initial admission and existing-session policy transitions.

Expanded the two-path policy-transition regression to assert durable reserve
and settlement records: new usage belongs to each path's new snapshot while
the unresolved old batch remains Unknown under its original snapshot.

Validation on this integration tree: Engine full go test -race ./... PASS;
Agent/Hub go test -race ./agent/... ./hub/... PASS; go vet for both scopes PASS.
This establishes a merged implementation baseline and local regression only,
not dedicated-host performance or D-04 dynamic-policy qualification.

A fresh read of dedicated-host formal-c6a0761 reports current-stage=complete,
formal-exit=0 and qualification-gate status=pass, reasons=[], trials=3,
warmup_seconds=300, with remote HEAD c6a0761. That performance evidence
remains scoped to c6a0761 and must not be attributed to this integration tree.
D-04 adaptive demand/idle policy and complete attack coverage remain open.

## Funded durable OPEN attack coverage

Extended TestRuntimeAutomaticallyUsesDurableLeaseAuthorityBeforeTarget with
five route forgeries for each of TCP and UDP after the real Hub-issued credit,
runtime lease and share lease are installed through authenticated control.
Future rule revision and path generation require OPEN class 7; skipped hop and
visited local node require class 1; duplicate visited node requires class 8.
Each case checks the received 18-byte OPEN_ERR including nonzero correlation
ID, no target access, and the durable source balance: initial=remaining=4096,
reserved=committed=unknown=0. The following successful stream and same-session
renewal ensure failed admission does not exhaust the sole connection slot.

Validation: go test -race ./agent -run
TestRuntimeAutomaticallyUsesDurableLeaseAuthorityBeforeTarget -count=1 PASS.
This is local real TLS/SQLite runtime coverage, not full multi-hop attack or
dedicated-host qualification. WaitOpen intentionally exposes only a formatted
error; assertions inspect the wire frame to verify the correlation ID.

D-04 integration audit: ReserveRuntimeUsage selects already installed grants
and renews session references atomically; Issuer.Issue durably deduplicates
explicit requests. No active demand-driven control loop connects these APIs.
Remaining implementation must add per-immutable-key rolling consumption,
coalesced persistent request identity and authenticated delivery, bounded
missing-authority wait, and proof-based idle drain. Do not reinterpret
selection of preinstalled grants as adaptive issuance or lock the candidates.

## D-04 durable request coalescing

Added Agent-owned authorization_demands (schema 12) and PrepareDemand /
CompleteDemand. An immutable Hub/delegation/node/boot/source/snapshot/rule/
revision/path/epoch scope has at most one pending request. Preparation commits
before returning a dispatchable UUID. Concurrent callers, changed amount/TTL,
process restart and a lost issuer response all retain the original request.
A 4096-pending cap rejects excess work without deleting unresolved requests.

Completion requires the matching installed authorization update and matching
credit record, including exact authenticated issuer, parent delegation, source,
boot, rule/revision, path, epoch, snapshot and amount. A repeated completion
with the same grant is idempotent. No timeout/expiry frees an outstanding issue,
installed grant balance, or unknown liability. These APIs do not grant authority.

Evidence: TestDemandCoalescesAcrossRestartAndLostIssuerResponse uses separate
Agent/Hub SQLite stores, 16 concurrent preparers, actual close/reopen, durable
Hub issuance/replay and source audit (256 issued once, 768 remaining). It tests
crash after installation, 10 mismatched response fields, repeated completion,
and next request identity. TestDemandPersistenceFailureReturnsNoRequest injects
a failed insert and requires no dispatchable request. Targeted race PASS;
Agent/Hub full race PASS before the additional mismatch assertions; final
targeted race includes all assertions. go vet ./agent/store PASS.

Not yet wired into the demand/control loop. Current generic control issuer
label must be replaced or bound to authenticated Hub identity before completion
can safely match DemandScope.HubID; do not weaken this check to integrate it.
Rolling consumption, adaptive target/waterline, 200 ms admission deadline,
proof-based idle drain and native qualification remain required. Parameters
remain candidates, and full D-04/attack qualification remains open.

## Atomic control-response completion and TLS issuer binding

PersistAuthorizationUpdate now resolves a matching durable demand in the same
transaction that installs its authorization records and balance. A scope or
issuer mismatch rolls back authorization records, balances, fencing changes and
request completion. An injected completion write failure similarly leaves no
partial authority. Unsolicited updates without a demand keep their prior path.
Replaying an already installed update validates the same completed request and
cannot retire a later demand.

The previous note about a generic control issuer is refined by code inspection:
ControlClient already derives issuer identity from verified TLS certificate
DNSName/CommonName. The older test fixture used the literal name hub; no
production identity check was weakened. A new fixture issues a certificate for
the UUID matching DemandScope.HubID and exercises the existing authenticated
control stream and real Hub issuer.

TestDemandCompletesOnlyFromBoundTLSIssuer passes: a different trusted TLS Hub
identity cannot install the response or clear pending demand; correct identity
installs 8 MiB once and completes the request; replay after preparing the next
request neither clears that request nor repeats the issuer debit. Store tests
now inject mismatched incoming records before installation, assert rollback,
and cover write failure at completion. Agent/Hub full race and vet PASS.

Scope: response handling is now connected to authenticated control. Outbound
request transport, rolling consumption/target/waterline scheduling, bounded
authority wait, idle proof protocol and full host qualification are still open.
No D-04 parameter lock or full attack qualification is asserted.

## D-04 rolling target candidate and transport proposal

Added DemandPolicy with explicit candidate configuration (no approved defaults).
It keeps sixty fixed per-second buckets of successfully committed raw bytes.
The current/previous 59 buckets produce ceil(minute_bytes * horizon_seconds / 60),
clamped to configured floor/ceiling. At or below the configured remaining
waterline it requests target minus available, never target plus held/unknown.
Arithmetic saturates instead of wrapping. A clock rollback faults the estimator
until explicitly reconstructed; losing its estimates cannot retire a durable
pending request. Scope isolation must be enforced by its owning controller.

Tests verify cold floor, a 100 Mbps minute -> 375 MB 30-second target, exact
30-percent waterline, full window expiry, rollback fail-closed, concurrent
observations, saturation and request cap. Targeted race and Agent vet PASS.
This helper is not yet connected to settlement or outbound scheduling; it is
not evidence of measured adaptive issuance or a parameter lock.

Prepared 04-13-DEMAND-CONTRACT-PROPOSAL.md with SHA-256
1dfd8ae92fb93217011118ccbd605067f94b8944ef9e257fcadeba8cfcbb2064.
It proposes control 1.3.0 demand and durable definitive-denial messages, using
existing AuthorizationUpdate for success. Owner approval requested because
contract-review.yaml explicitly requires owner approval for normative changes.
No frozen schema, approval hash or version has been modified. Independent
implementation continues; transport implementation awaits that decision.

## Approved control 1.3.0 and durable Hub demand outcomes

Owner explicitly approved in chat: “批准 1.3.0 提案”. Recorded approval at
2026-11-05T23:42:15Z for unchanged proposal hash
1dfd8ae92fb93217011118ccbd605067f94b8944ef9e257fcadeba8cfcbb2064.
The control schema is now 1.3.0 with CreditDemand, CreditDemandDenied and the
three definitive-denial enum values. Updated approval hash, version, changelog
and ENG-037/ENG-050/Agent/Hub mapping. Generated using pinned protoc 36.2,
protoc-gen-go 1.36.12, protoc-gen-go-grpc 1.6.2. Contract freeze/map/change,
reproducible generation and five contract checker unit tests PASS.

ResolveHubDemand uses the same transaction as issuer validation/issuance.
Insufficient funds, expired authority or invalid scope can produce a committed
non-issuance record. Invalid envelopes, unknown time, closed signer, conflicts
and storage errors produce no definitive response. Persistent denials prevent
the legacy IssueHubAuthorization API from later issuing that same identity.
Hub Issuer.ResolveDemand maps committed outcomes to the approved typed messages.

Tests cover denial/restart/replay, payload conflict, fresh smaller request,
write-failure without outcome, and sixteen concurrent successful replays with
exactly one debit. Agent/Hub full race PASS; vet and contract gate PASS. The
new Hub typed adapter builds and is not yet called by the stream dispatcher.
Agent denial processing/outbound delivery and adaptive scheduling remain open.
This approval and these finite tests do not lock parameters or close Phase 4.

## Typed demand delivery and Agent denial receipts

Opt-in ControlClient.EnableCreditDemand now dispatches durable pending requests
through its single-sender authenticated gRPC loop. It filters by actual verified
Hub UUID, current node and boot, pages at 32 requests, and rotates a cursor.
Default polling is one second; intervals below 100 ms are rejected. Requests
retain identity until success or definitive denial; transport errors never
remove them. This polling is not yet the candidate 200 ms data-path waiter.

The explicit test Hub DemandIssuer adapter handles CreditDemand and returns the
committed success or typed denial. Agent accepts known denial enums only, checks
the immutable Hub/node/boot binding, and persists denied state. Duplicate denial
is idempotent; different denial, late success after denial, and denial after
success conflict. No grant/balance/usage table is touched by denial processing.
Schema 14 transactionally migrates the previous pending/applied table, retaining
request identity and the one-pending-per-scope index.

The real mTLS test now sends actual requests: installed 8 MiB plus a requested
4 MiB, denied an 8 MiB request against the final 4 MiB, then requested 4 MiB
under a fresh ID. Final Agent initial=remaining=16 MiB and Hub issued=16 MiB,
remaining=0. Stale denial replay does not retire the next request. Additional
tests cover old schema migration, wrong node/boot/Hub and denial-write rollback.
Full Agent/Hub race, vet, reproducible protobuf generation and contract gates
PASS. No tests remained running at completion of this slice.

Still open: feed committed consumption and exact scoped available balance into
the rolling policy, admission wait/notifications, production startup wiring,
proof-based idle drain, delayed/lost-response workload, complete attack review
and dedicated-host adaptive qualification. No parameter lock is asserted.

## Adaptive request planning uses durable settlement and balance

Schema 15 adds a sixty-slot durable consumption ring for each complete immutable
demand scope. Successful known settlement adds actual bytes in the same SQLite
transaction; duplicates, returned bytes and unknown upper bounds do not add
consumption. Ring-write failure rolls the whole settlement back. It uses local
seconds for rate estimation only, rejects rollback, and never grants authority.

ReadDemandLedger reads that ring and exact scoped remaining grants in one
transaction. Credit must pass conservative trusted-time validity and source
lineage checks; Hub, delegation, node, boot, source, snapshot, rule/revision,
path and epoch must all match. Held and unknown bytes stay deducted. Missing
time trust prevents planning. Future not-before grants are not counted early.

Node.PrepareAdaptiveDemand now derives target/waterline from those actual
values and persists the resulting deficit. Opt-in ControlClient accepts at
most 64 trusted-provisioned adaptive scopes, plans only for the authenticated
Hub, and sends through the existing durable demand delivery. Bootstrap time
trust still requires an authenticated anchor before adaptive issuance starts.

Tests: actual reserve/commit/replay/unknown settlement, seven changed scopes,
rate expiry without fund release, injected rate-write rollback, 121 seconds
occupying exactly 60 rows, and clock rollback. Real mTLS fixture verifies a
64 MiB test-only target reads 16 MiB valid remaining credit and requests the
48 MiB deficit, then safely processes issuer denial. Initial test assumed a
just-installed grant was already time-valid; corrected it to wait for the
conservative earliest bound crossing not-before, without weakening checks.
Agent/Hub full race and vet PASS; additional bounded-window targeted race PASS.

Remaining: demand-driven wakeup and at-most-200ms data-path waiting, denial
backoff, production configuration wiring, safe idle shrink/reclaim, workload
scans with real durable issuer, full attack matrix and same-SHA native evidence.
The additional settlement transaction work has not yet been performance
qualified; c6a0761 performance cannot be reused for this implementation.

## Durable refusal backoff

Schema 16 persists refusal reason, streak and retry time per immutable demand
scope. Automatic preparation checks it in the same transaction as coalescing
and request creation. Funding denial uses 1,2,4,8,16,32,60-second capped backoff;
invalid/expired immutable scope stops automatic requests until provisioning
changes. Existing pending requests still replay exactly. Trusted explicit
preparation can submit a reduced request without redefining a pending one.

Denial-state update and retry-state update commit together. Duplicate denial
does not increment the streak or postpone the deadline. Only a newly completed
success clears refusal history; old success replay cannot clear a newer denial.
Clock rollback fails closed. Scheduling treats deferred retry as a normal skip
so usage/control processing continues. These retry guards do not constitute
D-04 parameter lock and are not the 200 ms data-path wait.

Tests cover restart, duplicate denial, pending coalescing during cooldown,
expired-scope suppression, cap/rollback, plus real mTLS replay of old success
after a newer denial. Full Agent/Hub race and vet PASS; additional mTLS
anti-replay targeted race PASS. Remaining waiting/idle/workload/qualification
requirements are unchanged and still open.

## Missing-authority retry and control wakeups

Runtime Open and session Reserve now retry a durable authorization denial only
when an opt-in control subscription matches the rule/revision/path. The Agent
coalesces wakeups per control consumer; control sends through its existing
single-sender loop with a 25 ms wake-send throttle. Subscribers are scoped to
the authenticated Hub and removed on disconnect. At most 128 callers wait;
excess callers retain immediate denial. Missing-authority retry uses a 200 ms
context and 10 ms transactional recheck cadence. A notification never grants
permission and transport timeout never retires the persisted request.

Unit tests exercise no subscriber, wrong scope, asynchronous availability,
200 ms retry deadline, cancellation and storage failure without retry. Full
Agent/Hub race and vet PASS. No actual end-to-end wait qualification is claimed:
the initial database attempt precedes the retry deadline, SQLite/context return
latency and rate-limit waits are separate, and scheduler delay can add latency.
Dedicated-host measurement must establish the total admission requirement or
drive further changes before D-04 can be locked. Idle proof protocol, production
startup configuration, full attack coverage and durable performance remain open.

## Initial authority attempt shares the retry deadline

withAuthorityWait now creates its 200 ms context before the first transactional
authority check and passes that same context to every retry. Already-cancelled
callers never enter the transaction. This supersedes the previous slice's
limitation that the first database attempt was outside the retry budget.
TestAuthorityDeadlineIncludesInitialAttempt consumes 120 ms on initial denial
and asserts the second attempt receives the exact original deadline. Existing
no-subscriber, wrong-scope, storage-failure and cancellation tests remain passing.
Full Agent/Hub race and vet PASS. This proves deadline propagation, not a
hard real-time latency guarantee: native SQLite cancellation/lock contention
and scheduler latency still require dedicated-host measurement. Rate shaping
waits remain distinct from missing-authorization waiting.

## Native durable-regression dispatch (382aab9)

Added actual SQLite contention tests. Local writer-lock admission failed with
SQLITE_BUSY in 364.375 microseconds without creating a runtime session or
changing funding. A held sole database connection caused queued admission to
respect its 40 ms context deadline with no session insert. Three race repeats
PASS; these are finite local tests, not native performance qualification.

Clean commit 382aab9f4283dfbd24c0fc38c8a2282b257a2a1d was transferred by Git
bundle to the dedicated Ubuntu host, cloned separately at
/root/VORTEX-phase4-durable-382aab9. Previous c6a0761 checkout and formal
performance evidence were preserved. Detached launcher
/tmp/phase4-durable-check.sh, PID 81041, runs ten admission repetitions, full
Agent/Hub/Engine race and vet. Output: artifacts/phase4/durable-382aab9/
(commit, git-status, host, stage, exit-code and per-scope logs). At dispatch
verification the live process was in admission dependency/build preparation.
Only a terminal exit-code and inspected logs can establish pass. No new
throughput or D-04 parameter qualification is claimed by this regression run.

Native run completed: stage=complete, exit-code=0, clean measured SHA verified. All ten admission repetitions, full Agent/Hub/Engine race and vet PASS. Logs copied to artifacts/phase4/durable-382aab9; hashes in 04-14-NATIVE-REGRESSION.json. PID 81041 terminal. This replaces the above in-progress status only; performance and remaining D-04 requirements remain open.

## Signer handoff demand outcome adversarial regression

TestDemandOutcomesRemainImmutableAcrossSignerHandoff creates a definitive
funding refusal and one successful issue, performs durable epoch handoff, then
opens a second Store and replays both. Refusal remains non-issuance, the old
success replays byte-identically, and only a fresh request uses epoch 2.
Changing a previously successful UUID to an oversized request is a conflict
and cannot create a contradictory denial row. Source audit remains 512 issued
plus 512 unissued from 1024 initial after two distinct 256-byte issues.
Targeted demand/handoff race and store vet PASS. This is a finite local test,
not complete attack coverage or a new native measured SHA.

Reconciled 04-06-D04-REVIEW.md with current durable implementation and gaps.
Explicitly separated unissued Hub remainder transfer from Agent-held grant
return, which lacks a complete fenced-unused-proof path. Historical parameter
scan does not qualify the durable gate's per-write reservation strategy.

## Local immutable grant fence for later idle proof

Schema 17 introduces sealed_grants. Explicit SealIdleGrant checks full demand
scope and rejects any open session referencing the grant, including its initial
anchor after renewal. It atomically changes an active credit grant to
settlement_only and records immutable scope. Replays match exactly. It does
not alter balances, acknowledge unused funds or invoke issuer reallocation.
Outstanding reservations may still settle under original grant identity, and
unknown upper bounds remain frozen. Existing authorization paths reject new
use of settlement_only grants after store reopen.

Tests cover active-session denial, repeated/conflicting seal, restart, direct
reservation bypass, late unknown settlement and write-failure rollback. Ten
concurrent seal/admission trials require exactly one winner. Targeted race
PASS; Agent/Hub full race PASS before adding concurrency assertions; additional
targeted race includes them. Store vet PASS.

This is a local prerequisite, not completed idle reclaim. Timed idle tracking,
1 MiB shrink strategy, complete unresolved-hold accounting, final unused proof,
authenticated transfer and exactly-once issuer credit remain required. No
wire contract was changed, and no native performance claim is added.

## Final local grant settlement snapshot

Schema 18 adds immutable grant_settlement_proofs. Preparation requires a matching
persisted seal and non-active grant, reuses grant-level conservation auditing
inside the same transaction, rejects any unresolved reserved batch, and binds
full scope plus initial/used/unknown/unused amounts to SHA-256. Retry validates
current ledger and stored bytes/hash rather than trusting a previous snapshot
blindly. No balance or liability is released by preparation.

Tests exercise unsealed grants, outstanding reservations, late known/unknown
settlement, exact unused exclusion of unknown bounds, store reopen, identical
replay, corrupted hash and injected write failure without exposed proof.
Full Agent/Hub race and vet PASS. This snapshot is local evidence only, not
authenticated wire proof or issuer credit. Timed idle lifecycle, shrink policy,
proof transport, Hub reconciliation/one-time recredit and adaptive performance
remain open. The control 1.3.0 approval does not imply an unused-credit return
message already exists; no frozen wire artifact changed in this slice.

## Persistent quiescence observation with activity invalidation

Schema 19 adds demand_idle_observations and transactional invalidation triggers
on runtime session insert/update, authorization hold insert/update, and demand
insert/update. Invalidation is conservative across the entire node/rule, so
activity on another path cannot leave an old idle interval accidentally intact.
ObserveDemandIdle refuses to count open sessions (including old boots), pending
holds or pending requests as idle. It computes minimum duration from the initial
latest time bound to the current earliest bound and durably restarts the
interval after backwards latest-time movement. It does not return funding.

Tests establish an interval, create and close a session entirely between scans,
reopen Store, and require duration zero. A pending direct reservation prevents
idle; unknown settlement starts a fresh interval while preserving unknown=100.
Clock rollback invalidates the old interval rather than resuming it later.
Targeted race, vet and full Agent/Hub race PASS.

Not yet an automatic shrink/reclaim controller: lifecycle observation must be
combined atomically with grant fencing, preserved warm allowance and final
proof dispatch before enabling timed policy. No parameter lock or performance
claim is made by these tests.

## Atomic conservative idle timer and grant fence

SealGrantAfterIdle now checks the persisted conservative idle interval, audits
reserved liability and seals the exact scoped grant in one transaction. Session,
hold and demand activity triggers invalidate the observation before a later
seal can consume it. Clock rollback or malformed timestamps durably invalidate
the interval; clock recovery cannot resurrect the old timer. Threshold remains
caller-supplied, not a locked D-04 default. No credit is returned to the issuer.

Tests cover the earliest-bound threshold (latest alone cannot authorize sealing),
activity wholly between scans, successful fence plus conserved unused proof,
post-fence admission denial, and rollback/recovery starting a fresh interval.
Agent packages passed full race tests; the initial combined command exited 1
because an incorrect ./edge-hub/... path does not exist. Corrected ./hub/... race
and go vet ./agent/... ./hub/... passed. git diff --check passed.

This is local lifecycle groundwork only. Warm allowance shrink, authenticated
proof delivery and one-time issuer reconciliation, durable adaptive performance,
full attack qualification and D-04 parameter lock remain open. Historical native
and throughput evidence is not evidence for this new SHA.

## Explicit node startup integration for adaptive demand

vortex-node now accepts --credit-demand-config and passes validated candidates
to its actual ControlClient, enabling the existing periodic/wakeup sender only
when the file is explicitly provided. Local JSON is bounded to 1 MiB/64 scopes,
rejects unknown fields/trailing objects, pins Hub and optional node identity,
forbids file-supplied boot identity, checks all scope and policy bounds, rejects
duplicates, and binds the current Node boot. No authorization is created by
configuration. Missing Hub settings or malformed config fail startup.

Race tests for cmd/vortex-node, Agent and Hub PASS; vet for those packages PASS;
git diff --check PASS. Parser tests cover current/restarted boot binding,
foreign identity, stale boot, bad UUID, missing scope fields, TTL/poll overflow,
invalid candidate policy, duplicate/excess/empty scopes and malformed/oversized
files. Existing authenticated demand integration regressions also pass.

See docs/operations/phase4-adaptive-demand.md for concrete candidate input and
bootstrap prerequisites. Actual Hub delegation provisioning and current-boot
authorization/time bootstrap are still required: this file does not solve or
bypass them. Full native process deployment, load/failure tests, idle return
protocol and parameter locking remain incomplete. No wire contract changed.

## Expanded funded runtime OPEN attack cases

`TestRuntimeAutomaticallyUsesDurableLeaseAuthorityBeforeTarget` now exercises
12 forged OPEN variants for both TCP and UDP after an authenticated, durable Hub
credit grant: unknown rule, stale/future rule revision, stale/future path
generation, skipped hop, local loop, duplicate/zero visited node, over-limit
visited list, invalid network, and unspecified original-client address. All 24
checks establish the expected rejection path, zero target access and unchanged
source ledger (initial=remaining=4096; reserved/committed/unknown=0).

The over-limit visited list and invalid network are rejected by the local
MarshalOpen preflight before a frame is sent. They are therefore client-side
protocol guards, not claims of remote runtime rejection; ParseOpen and OpenPolicy
protocol rejection have separate VLink test coverage. The other ten attacks
reach the real TLS/HELLO runtime, receive the expected OPEN_ERR class with a
nonzero correlation ID, and leave target and funds untouched. This extends
funded-route coverage from five to twelve types per transport. It remains a
finite route attack set, not a complete attack audit across certificates,
control replay, tenant isolation, quotas, disk failure, multi-hop switching and
recovery.

Verification: targeted funded-runtime race test PASS; full `go test -race
./agent/... ./hub/...` and `go vet ./agent/... ./hub/...` PASS; engine module
`go test -race ./vlink/...` and `go vet ./vlink/...` PASS; `git diff --check`
PASS. A combined root-module command with `./engine/vlink/...` was invalid
because Engine is a nested Go module; corrected commands ran at each module root.

## Actual adaptive demand drives funded TCP/UDP runtime renewal

The durable-runtime test now drives demand through the configured Agent
ControlClient after actual metered traffic, using a Hub TLS identity exactly
matching the delegation and immutable DemandScope. For each transport, one
1500-byte echo commits 3000 raw bytes across both directions from an initial
4096-byte grant, leaving 1096 below the configured 30% waterline. The live
controller computes a 3000-byte deficit from the durable one-minute consumption
ring and scoped available balance, sends control 1.3.0 CreditDemand, and the Hub
commits/issues exactly 3000 bytes before replying. The Agent persists and
installs the response, closes the request atomically, and observes exactly 4096
available after conservative NotBefore catches up. The same open TCP or UDP
stream then sends another 1500-byte echo after the original 4-second grant
expires. Old and new grant IDs each retain their two directional usage batches;
source and Hub conservation checks pass. The test no longer pre-issues the
renewal out of band.

This exposed and corrected a TestServer limit semantic: UsageBatchLimit=0 now
means no usage-count cutoff, allowing initial pending usage ACKs and a subsequent
demand to share one authenticated stream. This is test-harness behavior, not a
production server change. The earlier mismatched placeholder Hub certificate
was corrected so the authenticated TLS issuer, delegation and adaptive scope
are identical; exact identity isolation remains enforced.

Verification: targeted `go test -race ./agent -run
'^TestRuntimeAutomaticallyUsesDurableLeaseAuthorityBeforeTarget$' -count=1`
PASS for both TCP and UDP; full `go test -race ./agent/... ./hub/...` and
`go vet ./agent/... ./hub/...` PASS; Engine VLink race/vet PASS. This proves a
small-budget end-to-end trigger/issue/install/use cycle only. It is not a
sustained throughput/D-04 scan, lost-response test, or proof-based return path.

## Dedicated native adaptive integration regression — 4e2780d

Clean measured commit `4e2780de8a51d311e785656533ca5576936b7f83` completed on
43.254.164.7, Ubuntu 24.04.5 amd64, Go 1.27.3. Detached launcher finished with
stage=complete and exit-code=0. Three uncached race repetitions of the funded
TCP/UDP runtime test passed (36.565s total). This includes actual metered
consumption triggering a 3000-byte control 1.3.0 demand and same-stream use of
the new durable grant after initial expiry; 12 attack inputs per transport,
including two client-side encoding refusals, passed each repetition.

Full Node/Agent/Hub race tests and vet passed, followed by full independent
Engine module race and vet. Both socket max buffers remain at 212992; no sysctl
change was needed. Raw logs and final targeted-repetition Agent/Hub/backend
SQLite snapshots were downloaded into artifacts/phase4/durable-4e2780d.
04-15-NATIVE-ADAPTIVE.json records immutable SHA-256 for every downloaded file.
Snapshots are the final repetition only; runtime.log records all three.

This qualifies the finite native correctness regression at the named SHA only.
Sustained adaptive throughput, delayed/lost response workload, full attack scope,
idle return reconciliation and D-04 parameter locking remain unqualified.

## Lost control demand responses with actual runtime renewal

The protocol TestServer has an explicit test-only DropDemandResponse injection
immediately after ResolveDemand returns its committed outcome and before Send.
The funded TCP/UDP runtime regression now disconnects at that boundary for the
first adaptive renewal. Assertions establish Hub issued=7096/remaining=5192,
Agent initial=4096/remaining=1096/committed=3000 with no reserved or unknown
balance, and one pending request for exactly 3000 bytes. An explicit changed
8192-byte estimate still returns that exact original request including TTL.
A new authenticated control connection replays it, installs its existing grant,
and completes the pending request; the same live stream continues after old
grant expiry. Final Hub conservation checks prove no duplicate debit.

The demand TLS regression also drops an oversized request's persisted denial.
Until the denial arrives, a smaller requested amount cannot retire the original
pending identity. Hub issued=12MiB/remaining=4MiB remains unchanged. Reconnection
replays the denial, after which a fresh smaller request succeeds using only the
remaining 4MiB. Existing stale-response and refusal-backoff checks still run.

Targeted runtime race test PASS; full Agent/Hub race and vet PASS; diff check
PASS. These are local finite transport-loss checks with actual SQLite/TLS and
runtime TCP/UDP traffic. They do not establish sustained fault-load throughput,
process-crash response recovery, all attack classes or parameter lock. No
production protocol or frozen wire artifact changed.

## Reviewable grant-return contract proposal

04-16-GRANT-RETURN-PROPOSAL.md proposes additive control 1.4.0 GrantReturn and
GrantReturnAck. It binds a permanently sealed grant, final known/unknown batch
commitment and exact unused amount to a durable per-grant return receipt.
Hub reconciliation must match batch identities/hashes as well as totals before
one-time credit. Old ownership returns remain nonspendable recovered funds;
unknown liability is never returned. Agent receipt completion never creates a
new spendable grant. The proposal specifies replay, conflict, crash, transfer
and opt-in compatibility behavior plus concrete verification obligations.

Current local GrantSettlementProof only records totals: it is not sufficient
for this wire proof and no current Hub recredit implementation is claimed.
No frozen proto/hash/version was changed. The explicit owner-approval rule in
docs/governance/contract-review.yaml applies; the prior 1.3.0 approval covers
CreditDemand outcomes, not this new unused-credit return. Other performance and
attack work remains authorized while this proposal awaits an owner decision.

## Sealed grant replay and resurrection boundary

TestSealedGrantCannotBeResurrectedByAuthorizationReplay creates committed=80
and unknown=120 against a 1MiB grant, seals it, prepares a conserved final proof,
closes the database and reopens it. Identical update replay remains idempotent
without restoring spending permission. New envelopes reusing the grant ID,
including changed amount, boot, signer epoch, path generation or issuer, fail
with an immutable identity conflict. After every attempt, direct reservation
and authorization checks still deny use; source conservation and final proof
bytes/hash remain unchanged. A rejected path update cannot partially advance
the stored path generation. This checks the Store transaction boundary rather
than claiming new authenticated transport coverage or hostile-agent attestation.

Targeted seal/proof race tests PASS; full Agent/Hub race and vet PASS; diff check
PASS. No production behavior or frozen contract changed. The unapproved 1.4.0
proposal remains pending; this independent local seal invariant is required
regardless of any future return wire protocol. D-04 remains unlocked.

## Bounded control response replay state for sustained sessions

Inspection found ControlClient kept every response message UUID in an unbounded
per-connection map. Continuous usage ACKs and adaptive demands could grow it
indefinitely. controlReplayGuard now enforces a 4096-ID hard bound while retaining
all earlier IDs for duplicate detection. Invalid sequence/UUID, duplicate IDs,
and messages past the bound are rejected before business application and do not
mutate the guard. Connect exits and its existing cancellation/connection cleanup
runs; Run uses the existing bounded reconnect backoff. No old IDs are evicted to
silently weaken replay checks. Durable demand/outbox identities remain the
cross-connection recovery mechanism; the bound itself grants no authority.

Tests fill the exact bound, retry the oldest ID with a newer sequence, submit
100 additional unique IDs, assert unchanged bounded state, and verify malformed
or stale messages cannot poison the next valid sequence. Full Agent/Hub race and
vet PASS, including real TLS lost-success/lost-denial recovery and funded TCP/UDP
renewal; diff check PASS. The guard is connection-local resource protection, not
a new frozen wire message. Sustained-load reconnect latency at this boundary
still requires measurement; no throughput or full availability claim is made.

## Real TLS response-bound rotation and replay

TestControlRotationDoesNotApplyOverLimitGrantAndReplaysOnce sends 4096 distinct
valid control message envelopes over actual mTLS/gRPC carrying identical durable
authorization updates. The 4097th envelope carries a different grant in the same
source lineage. Connect rejects it at the identity-memory boundary before
business application, and the source audit remains initial=remaining=4096.
Reconnecting to a fresh authenticated stream replays the previously unprocessed
grant: initial=remaining becomes 8192. A second connection replay keeps 8192,
with no reserved, committed or unknown amounts. Both success and over-limit
paths use ControlClient, not only the standalone guard helper.

Targeted uncached race test PASS (6.568s overall); full Agent/Hub race and vet
PASS; diff check PASS. This validates manual connection-boundary replay and
exactly-once durable installation. It does not measure Run backoff, continuous
business traffic during rotation, or dedicated-host tail latency. Test fixtures
issue valid authorization payloads directly; actual durable Hub demand issuance
is covered by the separate funded runtime/lost-response integration tests.

## Durable per-write cost benchmark entry point

BenchmarkRuntimeDurableAccounting measures the existing Store runtime-session
reserve + deterministic known-usage payload + settle path for 1200, 16384 and
65536 raw bytes. Each calibration installs a finite exactly sufficient fixture
grant through PersistAuthorizationUpdate, with runtime/share leases, and uses
actual WAL/FULL SQLite. Timed operations include gate checks, rate-bucket work,
transactional pre-debit, settlement, durable consumption window and pending
usage outbox insert. Afterwards it closes the session and checks exact source
conservation and one pending outbox row per iteration. No SQL balance refill,
non-durable sync mode or disabled metering is used.

This is a serial, one-session Store microbenchmark with injected trusted local
time and a deliberately nonbinding rate share. It transmits no network bytes,
does not exercise Hub issuance, does not implement session authorization blocks,
and excludes startup/final audit time. MB/s is accounting capacity only, not
VLink throughput or a Phase 4 gate. Fixed-size payloads model write granularity,
not D-04 grant/block candidate choices. Use it to profile the existing per-write
cost before evaluating a different reservation strategy.

Local Darwin arm64 smoke: 100 iterations per size PASS with conservation/outbox
checks; roughly 0.61–0.67 ms/op and 2920–2922 allocations/op. Store vet and diff
check PASS. Native results must be recorded separately at a clean measured SHA.


## Dedicated-host durable accounting baseline — 2026-11-06

Ran `BenchmarkRuntimeDurableAccounting` at clean commit `d46ca8e793a9b7f79c5e65ca011401bdbd76bdde` on the dedicated Ubuntu 24.04.5 x86_64 host (Go 1.27.3, Intel Xeon E5-2630 v2, 12 online CPUs). The exact command was `go test ./agent/store -run '^$' -bench '^BenchmarkRuntimeDurableAccounting$' -benchtime=1000x -count=5 -cpu=1`. Socket buffer max/default values were all 212,992 bytes and were not changed.

All 15 samples passed. Median reserve-plus-settle operation times were 6.819 ms for 1,200 bytes (0.18 MB/s accounting capacity), 6.032 ms for 16,384 bytes (2.72 MB/s), and 6.460 ms for 65,536 bytes (10.14 MB/s). Each benchmark verifies SQLite WAL/FULL, exact source conservation after the run, and one pending UsageBatch outbox row for each operation. The complete outputs, clean SHA/status and checksums are retained in `artifacts/phase4/durable-accounting-d46ca8e/`; the manifest is `04-17-NATIVE-ACCOUNTING.json`.

The roughly 6 ms per-operation cost is sensitive to write granularity and motivates a measured comparison with bounded session blocks. This result remains a serial Store microbenchmark: it includes no network transfer, Hub issuance or session blocks and is not VLink throughput, a Phase 4 gate, or a D-04 parameter selection. The planned real five-process one-factor candidate scan and its TCP/UDP, latency, allocation and conservation measurements remain outstanding; D-04 remains unlocked.

## Native seven-case D-04 diagnostic scan — 2026-11-06

At clean measured SHA `d46ca8e793a9b7f79c5e65ca011401bdbd76bdde`,
`tools/phase4/d04_run.py --duration 15 --repeats 3 --rate-mbps 100`
completed all 21 five-process trials on dedicated Ubuntu. Each trial uses
16 TCP streams, 50 short-flow samples and 100 Mbps target UDP. All trial
reports have runtime_correctness=pass, exact source conservation and settlement,
zero UDP loss, and actual UDP offered rate at least 99.9584 Mbps. Formal
performance qualification remains not-qualified: each workload phase is only
15 seconds and this scan supplies no independent TLS comparison.

Mixed TCP medians by case (Mbps): baseline 1026.43; 64 KiB start 1022.59;
1 MiB start 1025.54; 10-second target 1025.00; 60-second target 1026.78;
20% waterline 1024.50; 50% waterline 1022.47. These closely grouped diagnostic
results do not establish a statistically supported winner or lock defaults.
The issuer is the synchronous process-local finite allocator. Target seconds
are converted from a fixed 100 Mbps reference; this is not the persistent
Hub's rolling-rate demand scheduler or durable session-block implementation.

The initial unbuffered baseline failed closed: 3,874 kernel RcvbufErrors,
2.4801% UDP loss and a destination accounting mismatch. Its original directory
is retained and excluded from the successful scan. A separate buffered
preflight passed. The 21-trial scan used rmem_max/wmem_max=67,108,864 with
defaults still 212,992. Its EXIT cleanup restored both max values to 212,992;
scan and restore exit codes are both 0 and live sysctl confirmed restoration.

`04-18-NATIVE-D04-SCAN.json` records outcomes and archive hashes. Downloaded
reports and failure/preflight diagnostics are in
`artifacts/phase4/d04-scan-buffered-d46ca8e/`; the reports archive hash was
checked against the remote archive and all downloaded original files matched
the remote per-file manifest. Full role logs and accounting JSONL remain on
the dedicated host at the same relative artifact path under
`/root/VORTEX-phase4-accounting`; their hashes are in raw-artifact-sha256.json.
Certificate private keys were excluded from this evidence transfer.

Approval reconciliation: control 1.3.0 frozen-hash and requirements-map checks
PASS. The local complete generator check stopped at the exact-version guard
because protoc is 36.0 rather than required 36.2; it is not a passing generator
reproduction result. The approved proposal text remains immutable under its
recorded hash. Control 1.4.0 grant-return proposal is still unapproved.
