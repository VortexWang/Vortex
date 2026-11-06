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

## Continuous real-traffic durable renewal integration

`TestRuntimeContinuousDurableAdaptiveRenewal` reuses the funded TCP/UDP runtime
fixture and keeps the original business stream plus one actual mTLS control
connection alive for eight additional consumption-triggered renewals. Each
renewal is derived from the durable Agent demand ledger and issued by the
transactional Hub issuer; no renewal grants are injected. After the original
4-second grant expires, its 1096 unused bytes do not count toward a new usable
target: the first new demand requests 4096, subsequent demands request 3000.
The test audits source conservation with zero reserved/unknown bytes after
each exchange, verifies no pending demands remain, and reconciles Hub funding.
Per transport, total issuance is 32192 bytes and committed usage is 30000 bytes.

The initial expanded test revealed an invalid fixture assumption that only two
usage batches would remain pending when a demand-limited control stream ends.
Usage delivery is asynchronous. The fixture now stops traffic, reads the actual
durable pending set, and replays all of it through the Hub usage relay before
asserting an empty Agent outbox. Both Hub and protocol-backend SQLite ledgers
must contain exactly 20 committed batches. Existing single-renewal and disk-full
variants retain their respective checks. Continuous-test snapshot names are
separate so they cannot overwrite single-renewal evidence.

Targeted TCP/UDP old+new integration race run PASS (40.547s); full Agent/Hub
race PASS (Agent 52.137s, Store 11.332s; remaining packages cached), vet and diff
check PASS. This is local Darwin correctness evidence for eight controlled
renewal cycles, not a sustained throughput/latency qualification, full real
backend integration, multi-hop durable performance or D-04 parameter lock.
Dedicated Ubuntu execution at the new clean SHA remains to be recorded.

## Native continuous-renewal failure and authorization-read optimization

Dedicated Ubuntu clean SHA 4f41cee failed the continuous TCP and UDP race
integration after 27.986s. Both streams received RST during renewal; the UDP
bridge explicitly reported authorization denied with context deadline exceeded.
The launcher stopped before full regression/vet. Original logs remain at
/root/VORTEX-phase4-accounting/artifacts/phase4/continuous-4f41cee. This is a
failed native result despite the earlier local success, not qualification.

Inspection found readAuthorizationTx queried the same authorization row twice,
with a separate query solely for issuer_id. Fetch issuer_id with the existing
row query, retaining all parsing and authority checks. This reduces duplicate
SQLite work without changing the 200ms bound or bypassing durable metering.
Add per-round integration logs for diagnosing the native failure. Full local
Agent/Hub race PASS (Agent 50.722s), vet/diff PASS. Native revalidation is still
required; this optimization is not yet proof of the timeout root cause or fix.

## Native repetition disproves complete timeout fix

At clean cb69c03 the native continuous integration, full Agent/Hub race and vet
first passed (full Agent 89.422s, Store 37.750s). Three additional uncached
continuous race repetitions then yielded one FAIL and two PASS: UDP round 8
received RST with context deadline exceeded in the failing repetition. Therefore
the duplicate-query optimization does not establish a complete timeout fix.
Do not promote this run to stable qualification.

Both the 4f41cee failure and cb69c03 initial/repeat results, including initial
successful ledger snapshots, were retrieved as
artifacts/phase4/continuous-native-results/results.tar.gz. Archive SHA-256
verified against the dedicated host:
a17c00f7dda82279a7d0d67c16cd23c36b7c9c5ebe166f4634a19f3fbde3440e.
The first launch exit is 1 at 4f41cee; cb69c03 initial exit is 0 but repeat exit
is 1. Those scopes remain separate.

The next optimization removes eager full-row reads for every live historical
authorization during each pre-debit. Selection reads bounded ID/issue-time
metadata, parses/sorts actual timestamps as before, and reads full candidates
only until credit/runtime/share choices are validated. The 4096-candidate limit,
conservative time checks, family/issuer/source restrictions and atomic debit
remain intact. A regression exhausts the newest grant through a real hold and
requires fallback to a funded original grant while keeping the newer runtime
and share leases; exact conservation is asserted. Full local Agent/Hub race
PASS (Agent 48.384s), updated Store uncached race PASS, vet/diff PASS. Native
repetition of this new change is pending; the timeout issue remains open.

## Native 7ff8320 and bounded stage diagnostics

Clean 7ff8320 first passed continuous TCP/UDP race (37.791s), full Agent/Hub
race (Agent 89.317s, Store 38.162s) and vet. The subsequent three full TCP/UDP
repetitions passed twice, then failed on UDP renewal round 7 with RST and
`store: durable authorization does not permit this operation: context deadline
exceeded` (combined command 112.886s, exit 1). The on-demand candidate reads
therefore do not establish closure of the intermittent timeout.

Added error context to ReserveRuntimeUsage only for deadline/cancellation
errors: operation stage and elapsed time, preserving the original wrapped
error and all timeout/authorization behavior. Stages distinguish transaction
acquisition, session/lineage/current authority reads, existing hold lookup,
authority selection, session limit/update, reservation and commit. No timeout
increase or accounting bypass is introduced.

The instrumented uncommitted source was sent to the same native worktree for
explicitly diagnostic UDP-only repetitions: 3/3 and then 6/6 passed. No timeout
stage was captured. These diagnostic runs do not overturn the clean-SHA full
sequence failure or establish a fix. Local Store uncached race and vet PASS.
The native diagnostic source was downloaded and byte-compared with the local
source; logs and successful initial ledger snapshots are archived at
artifacts/phase4/continuous-7ff8320-results/results.tar.gz with remote-verified
SHA-256 344eb2ada3a066d991f7201ad69fac4e1480f0d7848ffa45eed6522a9dc9b54d.
Host socket maxima remained 212992. D-04 timeout investigation stays open;
next diagnostic should retain complete TCP/UDP ordering and inspect the failing
reservation stage instead of treating more passing UDP-only runs as closure.

## Longer renewal reproducer and zero-balance planning read

Clean d70fdaf passed three complete TCP/UDP eight-renewal repetitions. Extending
the fixture to 32 renewals then reproduced the native failure with actionable
error context: runtime reservation stage=select-authority elapsed=203.133214ms.
That run failed after 60.19s. This confirms where the deadline expired, not an
isolated timing measurement of selection itself (elapsed starts at reservation).

ReadDemandLedger was still fully checking every active historical credit grant,
including exhausted grants, on the shared SQLite connection. It now reads
ID/remaining metadata in one snapshot and skips authority/lineage queries for
exactly zero balances. All records still count toward the 4096 bound; positive
balances retain scope, conservative time, lineage and amount checks. A regression
exhausts an entire grant into unknown liability and verifies zero available
credit, zero consumption-rate contribution and unchanged unknown conservation.
This is a query-cost reduction, not a block allocator or liability refund.

The first local 32-renewal run also found only 32 of the expected 68 batches at
the protocol backend. Hub retained all batches: its initial backend heartbeat
had aged beyond the 30-second online grace, so later use correctly followed
autonomous local-ACK behavior. The online long-run fixture now records explicit
backend heartbeat observations each renewal while preserving its initial
absolute autonomy deadline. This is test liveness input, not production backend
heartbeat transport. Snapshot names include renewal count to avoid collisions.

With both changes, TestRuntimeLongLivedDurableAdaptiveRenewal passed locally
under race (102.661s) and on the dedicated Ubuntu host as an uncommitted-source
diagnostic (108.10s). Each TCP/UDP session completes 32 renewals, issues 104192
bytes, meters 102000, clears pending demands and Agent outbox, and verifies 68
Hub/protocol-backend ledger batches. Full local Agent/Hub race PASS (149.181s
Agent, 11.645s Store), vet/diff PASS. Native clean-SHA repeat verification remains
required; no stable timeout closure, performance qualification or D-04 lock.

Clean-stage results, before/after long-run logs and exact diagnostic source are
retained in artifacts/phase4/long-renewal-diagnostics/results.tar.gz. Remote
archive hash verified: 1349db5389998b23ed53c97d8e675e09aa5a982ac01c2122d4667acbed82b7f1.
Downloaded diagnostic files were byte-compared with local source.

## Clean native 32-round renewal qualification slice — 23a9ea3

Dedicated Ubuntu at clean commit 23a9ea3f1d8a8b499dc637d47438b4f57d1cea60
completed three independent uncached race runs of
TestRuntimeLongLivedDurableAdaptiveRenewal: 106.998s, 108.912s, 107.975s.
Each run includes TCP then UDP, 32 consumption-driven renewals per transport,
with separate saved Agent/Hub/protocol-backend SQLite snapshots. All three
passed, followed by full Agent/Hub race (Agent 196.602s, Store 38.693s) and vet;
launcher stage=complete and exit=0. No 200ms timeout adjustment or socket-buffer
change was made. Prior failures remain recorded, not replaced by these results.

Independently opened all 18 saved databases read-only after decompression,
verified original snapshot hashes and PRAGMA integrity_check, and compared
batch IDs plus payload hashes across each Agent/Hub/backend triple. All six
triples have exactly 68 matching batches, Agent state hub_acked and Hub/backend
state ledgered. Each Agent has initial=104192, remaining=2192, committed holds
=102000 with no other hold states, no open sessions and no pending demands.
These totals include the initial exchanges plus the 32 renewal rounds.

All 35 downloaded evidence files match their remote SHA-256 manifest.
`04-19-NATIVE-LONG-RENEWAL.json` records the measured SHA, six snapshot audits,
checksums and scope. Raw logs, 18 SQLite archives and audit results are at
artifacts/phase4/long-renewal-23a9ea3. Full transfer archive SHA-256 is
0d7fbbca87dd9f18d2af2d4fb95434da2eab80fd0562d9f0f6b63e78e6588ae1.

This closes the pending same-SHA native validation of the 32-round controlled
renewal slice. It does not prove arbitrary long-run latency, eliminate every
possible timeout, implement durable session blocks, cover the complete attack
matrix, supply production backend transport, or lock D-04 parameters. Broader
qualification and grant-return contract approval remain separate open work.

## Runtime reservation mutation/replay attack matrix

TestRuntimeReservationScopeAndReplayAttacksAreAtomic exercises 12 mutations
against an existing funded, unresolved reservation: node, boot, session, flow,
snapshot, rule, revision, path generation, invalid direction, zero amount,
changed replay amount and changed replay direction. Each must fail without
changing source balance, durable rate tokens/tick, hold/reservation row counts
or selected session authority. The original request must still replay once
without another debit. After explicit session close the same request is denied,
and its unresolved hold stays occupied rather than being refunded.

Targeted race PASS (2.834s); full Store uncached race PASS (11.149s); Store vet
and diff check PASS. These are local Store-seam attack assertions, not remote
OPEN, cross-tenant multi-hop or native hostile campaign evidence. No production
logic, frozen contract or parameter default changed. Durable session blocks
remain unimplemented: their implementation must preserve these per-session
identity and replay invariants as well as crash upper-bound freezing, revocation
and per-write rate enforcement.

## Delayed durable demand responses across active TCP/UDP sessions

TestRuntimeDelayedDurableAdaptiveRenewal adds eight real consumption-triggered
renewals per transport with a deterministic test-server response barrier. The
Hub resolves and commits each demand before announcing the barrier; its issuer
transaction is no longer held while the response is withheld. The test verifies
Hub issuance has advanced but Agent initial credit and committed consumption
have not, with zero holds/unknown usage, and exactly one original pending demand.
A changed 8192-byte estimate cannot replace that request. Releasing the barrier
allows installation and another metered exchange on the same business stream.
Final usage/outbox/ledger assertions are shared with the continuous fixture.

The callback is an explicit protocol TestServer hook, not production message
or grant semantics. Cancellation unblocks both barrier directions. Snapshot
labels include delay mode so evidence cannot overwrite ordinary runs.
Targeted local race PASS (30.760s); full Agent/Hub race PASS (Agent 184.662s,
Store 12.281s, Hub 2.795s), vet/diff PASS. This tests delayed delivery and durable
coalescing; business writes deliberately resume after delivery, so it does not
prove latency or availability while traffic waits on missing authority.

04-20-DURABLE-BLOCK-IMPLEMENTATION.md records the concrete remaining block
implementation boundaries. Existing pre-write reservation also consumes rate,
so merely enlarging it is not a correct block allocator. Funding holds, bounded
partial checkpoints, concurrent suballocation, revocation/time validation and
old-boot unknown recovery must be integrated before measuring block candidates.
No block implementation, parameter lock or Phase 4 completion is claimed.

## Native delayed-response and reservation-attack evidence — 69f5db9

Dedicated Ubuntu clean commit 69f5db9426946b267b400d8279c0833b090b9244:
three uncached race runs of TestRuntimeDelayedDurableAdaptiveRenewal PASS
(31.81s, 33.60s, 33.26s), including eight delayed renewals per TCP/UDP transport.
Each checks a funded Hub issue, unchanged Agent credit before delivery, one
retained demand identity despite a changed estimate, then resumes actual metered
traffic. Six transport runs each finish with issued=32192 and metered=30000.
Three repetitions of the 12-case Store reservation mutation/replay matrix PASS;
Agent/Hub vet PASS; launcher stage=complete, exit=0. Buffer maxima stayed 212992.

04-21-NATIVE-DELAY-ATTACKS.json records source SHA, scope and raw-file hashes.
Raw results are in artifacts/phase4/delay-69f5db9. The downloaded archive matches
remote SHA-256 040e4b79ec120bbf9647cb09d07e911877cdb7f0ae96058dc408bf870b81cf42.
Both logs were checked for the expected pass counts and absence of failed tests.

This is a finite controlled-response-delay and Store attack slice. During the
response barrier the test inspects ledgers but does not attempt unfunded writes;
it therefore does not establish wait-tail latency or traffic availability during
missing authority. Store mutation tests are not remote multi-hop attack proofs.
Durable session blocks, complete hostile matrix and D-04 lock remain open.

## Durable funding reservation — Store API only

Added ReserveRuntimeFundingBlock with atomic funding/marker persistence, one
unresolved block per session/direction, a 4 MiB block cap and 4096 unresolved
blocks per node. It skips rate consumption only for funding allocation; normal
write APIs reject replay of its batch identity. Scope and live session checks
remain in the existing transaction. Unresolved blocks use old-boot recovery's
full unknown bound; no unknown credit is returned.

Tests cover idempotency, direction separation, ordinary rate enforcement,
marker failure rollback, close/reopen recovery, unknown refund rejection,
oversize and closed-session denial. Local full Agent/Hub race PASS (Agent
181.750s, Store 12.945s; other packages cached); separate full Store race PASS
12.432s. go vet and git diff --check PASS. These are local results, not native
dedicated-host qualification.

No consumption/checkpoint API or Engine runtime wiring is enabled. Therefore
this is partial step 1 of 04-20, not durable block performance evidence, a D-04
parameter lock, or Phase 4 completion. Control 1.3.0 approval remains recorded;
this additive local schema/API change does not implement the unapproved 1.4.0
grant-return wire.

## Atomic funding-block checkpoints — Store persistence slice

CheckpointRuntimeFundingBlock splits confirmed consumption into a committed
child batch while reducing the parent residual in the same SQLite transaction.
The transaction also records the usage outbox and demand-consumption window.
Exact retries compare parent identity, prior residual, child metadata, amount
and payload. Stale residual updates fail. Fully consumed blocks leave a zero
returned tail with no refund; only the remaining residual is recovered unknown.
The existing settlement implementation is shared inside the transaction.

Targeted funding/block race tests PASS (2.383s), including injected outbox insert
failure after the split, complete rollback, exact retry, changed-payload and
stale-residual rejection, close/reopen recovery, replay after unknown recovery,
multiple partial checkpoints, exhaustion and reuse of the direction slot.
Full local Agent/Hub race PASS (Agent 182.120s, Store 14.638s, Hub 1.842s;
identity/upgrade cached). go vet and git diff --check PASS.

These tests prove a local transaction/recovery slice, not actual power-loss,
SQLITE_FULL or multi-hop runtime behavior. No runtime caller enables this API.
Exclusive suballocation, per-write admission/control-update serialization,
bounded flush scheduling and native qualification are still required before
D-04 can be locked. The goal and full Phase 4 qualification remain open.

## Funding-block write admission and finite attack slice

Added AdmitRuntimeFundingBlockWrite: a single transaction validates exact block
identity/residual, current open session, current and held grants, runtime/share
leases, immutable grant family, connection count and shared durable rate. It
uses the session's persisted lease IDs, not caller replacement IDs. Admission
spends rate only, without debiting already-reserved block funds a second time.

Sixteen local rejection cases PASS: boot, flow, snapshot, direction, grant,
session, revision, path, residual, untrusted time, expiration, closed session,
revoked grant/runtime/share, and resolved block. Each checks unchanged funding
and no rate bucket creation. A separate test mixes block and ordinary writes
and proves the shared rate cap remains enforced without double funding debit.
Targeted race PASS 3.253s; full Store race PASS 13.379s; Agent/Hub go vet and
diff checks PASS. The new API has no runtime caller, so this turn's validation
is Store scope; the previous full Agent/Hub run remains scoped to d8929c4.

No concurrent child allocator or Engine wiring is claimed. This API charges
rate on each call, not idempotent admission retries; ambiguous commit errors
must fault the allocator. SQLite rate persistence still costs a write for each
admission. Native performance, full multi-hop hostile coverage, and D-04 lock
remain open.

## Experimental Agent funding-block gate

Added explicit NewExperimentalFundingBlockGate implementing the existing Engine
accounting interfaces. Per-session mutex ownership bounds aggregate concurrent
reservations by each direction's durable residual. Capacity wait supports
cancellation. Each confirmed write checkpoints actual bytes into the existing
UsageBatch protobuf/outbox; short-write tails stay available inside the hold.
Close forbids new admission and waits for outstanding callbacks before returning
the corresponding unused tail. Unknown outcomes freeze the full residual and
fault the gate. Checkpoint failures leave the durable bound for boot recovery.

Interface-level tests PASS: 16 concurrent short writes with exact ledger and
protobuf outbox totals; independent directions and close-before-resolution
without premature refund; unknown residual freeze and admission stop; canceled
capacity wait without over-allocation. Targeted race PASS 2.426s. Full local
Agent/Hub race PASS (Agent 178.288s, Hub 2.077s; Store/identity/upgrade cached).
Agent/Hub vet and diff checks PASS.

The default Node runtime still installs the per-write gate. This is explicit
experimental integration at the Engine accounting interface, not verified real
VLink transport or native performance. Every outcome currently checkpoints
immediately and every admission persists rate. Adaptive funding size, idle
return, renewed-authority wait/retry, real TCP/UDP renewal, the complete hostile
matrix and dedicated-host qualification remain open. No D-04 lock is claimed.

## Experimental block renewal, smaller allocation and idle return

Funding-block Reserve now uses the existing scoped demand notification and
200 ms missing-authority deadline. An existing idle block denied admission is
returned before one fresh allocation attempt, allowing current valid leases
and grants to replace expired ones without moving past usage. If target-size
allocation is denied, the exact current write size is retried with identical
Store authority checks. No unreserved byte or cross-scope credit is used.

Idle residuals use a generation-checked one-second local timer. Admission stops
the timer; outstanding writes and poisoned blocks are never refunded. Returned
or committed callbacks re-arm only when no writers remain. Close cancels timers.
An idle settlement failure faults the gate and retains its durable liability.

New tests verify no refund during an outstanding write, eventual idle return,
reallocation after return, exact consumption of a final 96-byte balance below a
4000-byte target, and a live session using a renewed grant after old expiry.
The renewal fixture initially lacked an authenticated issuer and was correctly
rejected by the Store source-ownership check; both initial and renewed updates
now name the same issuer. No production check was weakened. Outbox inspection
confirms new usage references the new grant.

Targeted funding-gate race PASS 2.904s. Full local Agent/Hub race PASS (Agent
176.630s; other packages cached). Agent/Hub vet and diff checks PASS.
One second is an experimental value, not D-04 parameter selection. These are
Agent interface/Store tests, not live VLink or dedicated-host qualification.
Full block-mode hostile coverage, actual renewal transport and performance
remain open. Local hold return is not the unapproved control 1.4.0 Hub return.

## Real block-mode VLink renewal and directional fragmentation fix

The existing actual TCP/UDP VLink fixture now explicitly installs the block gate
before Engine start. Each mode covers initial signed authority, rejected OPEN
attacks and connection-limit denial, real bidirectional traffic, lost-response
demand replay, original authority expiry, two continuous renewals and final
Agent/Hub/protocol-backend ledger reconciliation. A second mode delays each
renewal response after durable Hub issuance and verifies unchanged Agent credit
and stable pending-demand identity before delivery. Snapshot filenames include
block mode to prevent overwriting per-write evidence.

First execution failed both transports: idle opposite-direction funding stranded
bytes needed to fund the response. The allocator now returns only proven-idle,
non-poisoned same-session blocks before retrying authoritative funding. A direct
regression proves in-flight opposite-direction funds are not reclaimed.

Two sequencing assumptions also failed: held idle credit can make a renewal
request larger than 3000 bytes (still bounded by its configured 4096-byte target),
and a proactive next grant may arrive before the prior round finishes waiting
for idle tails. The fixture now reads actual funded issuance, enforces per-round
3000..4096 bounds (first round exactly 4096), excludes the expired initial 1096
bytes from usable credit, and verifies exact final Agent/Hub issuance, known
payload consumption, zero reserved/unknown balance and source conservation.
Intermediate monotonic issuance does not weaken the final exact assertions.
This timing/occupancy behavior still needs measurement for D-04 selection.

Ordinary target race PASS 16.883s; delayed target PASS 16.965s. After the proactive
delivery sequencing fix, both modes repeated three times PASS 87.281s (12 total
TCP/UDP subcases). Final full local Agent/Hub race PASS (Agent 214.264s; other
packages cached), vet/diff PASS. Initial failures and passing raw logs are kept
in artifacts/phase4/block-integration-local with checksums.json. These are local
results, not dedicated-host performance or the complete hostile matrix. Native
block qualification, long-run/fault coverage and D-04 parameter lock remain open.

## Native block attempt e3662bc — failed UDP response scheduling

Dedicated Ubuntu clean e3662bc0710210d1f765a3020dffeff1216c3429 ran the
ordinary/delayed real block tests. TCP passed both; UDP reset on the first
exchange after old-authority expiry in both. Launcher stopped in trial-1 with
exit 1; attacks/full-race/vet were not reached. Evidence is retained remotely
in artifacts/phase4/block-e3662bc. Host stayed at socket maxima 212992.

Inspection identified a possible scheduling path: UDP target response can
request reverse funding before the outbound commit callback acquires the
session mutex. Its outstanding upper bound cannot be reclaimed, but rejecting
immediately can reset a fundable response. The adapter now waits on outstanding
opposite-direction resolution, then re-enters the actual funding/admission
checks. Cancellation still ends the wait; unknown settlement faults the gate.
A regression holds the outbound batch unresolved and proves the response waits
until its checkpoint, then reconciles exactly 3000 consumed / 1096 remaining.
Native confirmation of this fix is required before closing this failure.

## Native response-wait fix 82ae28b — still failed; timeout retained

Local funding-gate race PASS 3.135s; both real block renewal modes PASS locally
27.689s; vet/diff PASS. Dedicated clean
82ae28b9a8283e0d537fb3be9850bee19120ef9d still failed the UDP expiry exchange
in trial-1. The ordinary UDP log identifies runtime reservation stage=reserve-
usage elapsed=93.778062ms with context deadline exceeded. This is one nested
transaction inside the unchanged shared 200 ms admission budget, not the total
operation elapsed time. Multiple funding attempts and tail release can consume
the shared budget. The outbound-wait regression establishes a scheduling
boundary but does not close this native failure. Atomic preferred/minimum block
selection and fewer repeated authority transactions are the next work.

Both native launchers stopped with exit 1 at trial-1; later attacks/full-race/vet
stages did not run. Neither result is a pass. Raw logs and completed TCP ledger
snapshots were downloaded. Archive SHA-256 matches remote
b41827d66884272766255a247d2a04ce2f6f8b6e0705aae565c839f31068f993.
04-22-NATIVE-BLOCK-ATTEMPTS.json records measured SHAs and file hashes.
Socket maxima stayed 212992. No timeout increase, kernel tuning or protocol
change was made. D-04 and full native block qualification remain open.

## Atomic preferred/minimum block selection and idle replacement

ReserveRuntimeFundingBlockUpTo selects a live grant with at least the write
minimum, then reserves min(preferred, available) inside the same transaction.
The chosen amount comes from the balance already read during authority
selection. Durable request metadata pins minimum, preferred, allocated amount
and the idle-return request digest. Exact retries retain the old allocation;
changed bounds, changed return identities and strict-mode replay are rejected.

ReserveRuntimeFundingBlockReplacingIdle returns at most one proven-idle
opposite-direction same-session tail in that transaction before selecting new
funding. Failure rolls back both the return and replacement. Agent supplies only
joined, non-poisoned tails and removes them from memory only after commit.
This removes separate target-size failure, tail-return commit and smaller-size
retry transactions from the new-block path. Per-write rate admission and the
200 ms deadline remain unchanged.

Initial Agent real renewal/funding race PASS 31.309s; after adding request
digest pinning, Store funding/admission race PASS 3.967s and Agent/Hub vet PASS.
Tests include below-target allocation, idempotency, request mutation, strict
replay rejection, impossible replacement rollback, injected request-marker
failure and exact final conservation. Native confirmation and full regression
for the final commit remain required. No D-04 parameter selection is claimed.

## Atomic funding native confirmation b02a394 — partial improvement, still fail

Final local full Agent/Hub race PASS (Agent 209.303s, Store 15.944s, Hub 2.348s),
vet/diff PASS at b02a394ca8f2abfd0539df5de4d3b5c8181793ea. Dedicated Ubuntu
trial-1 passed all four ordinary/delayed TCP/UDP cases in 38.086s. Trial-2
failed delayed TCP at the first exchange after initial expiry (RST payload 0008);
its other three cases passed. No underlying error was captured for that reset,
so it is not proven to share the previous timeout root cause. Launcher stopped
at trial-2, exit 1; trial-3 and later attack/full-race/vet stages did not run.

Archive download verified SHA-256
09f58d4e4108dfc4b8fad99ef2ea47b868890121cf1f8cab6a758a2f2278b127.
04-22 manifest includes raw trial logs and completed ledger snapshots. Host
conditions and the 200 ms gate remained unchanged. Native qualification remains
failed, despite the first successful complete native trial.

Added bounded-detail failure logging at the Agent block admission boundary
(direction, requested bytes, total elapsed and returned error) so subsequent
resets can be attributed without weakening authorization or timeouts. No payload
or credential is logged. Diagnostic change targeted funding-gate race PASS
3.281s; vet/diff PASS. Its native reproduction remains the next step.

## Native admission diagnosis and atomic first-write admission

Dedicated ec07301 diagnostic repeat (five ordinary/delayed runs) failed, exit 1,
157.926s. Boundary logs now confirm right-to-left 1500-byte admissions ending
at 200.7..202.0ms with context deadline exceeded. These observations identify
the unchanged total admission budget, not a TLS/frame correctness failure.
Raw results remain in artifacts/phase4/block-ec07301 on the dedicated host.

ReserveAndAdmitRuntimeFundingBlock now commits new-block funding, joined idle
tail replacement and shared rate debit for the first write in one transaction.
Existing-block writes still use fresh transactional admission. New allocation
already verifies session, current authority, lineage, connection limits and
held grant; avoiding the immediately repeated admission transaction preserves
these checks and removes an extra FULL commit. First-admission batch identities
cannot replay; uncertain commit errors must fault the caller.

Local Agent funding/real-renewal race PASS 32.012s; Store targeted race PASS
4.335s after adding first-admission replay/rate/rollback assertions. Agent/Hub
vet and diff checks PASS. Native and complete regression remain outstanding.
No timeout increase or D-04 lock is claimed.

## Native b2bdd42: renewal slice passes; concurrent admission still fails

Dedicated clean b2bdd42b00ef94cf62910943d44a98b5e33b9540 passed all three
independent ordinary+delayed TCP/UDP renewal trials (37.515s, 37.470s, 36.756s).
Store funding/admission attack tests repeated three times PASS 35.730s.
Uncached full native suite then failed TestFundingBlockGateConcurrentShortWrites
AndClose: concurrent same-session admissions exhausted the unchanged 200 ms
budget. Agent failed 273.639s; Store passed 55.769s, Hub 1.930s, identity and
upgrade passed. Launcher exit=1 at full-race; native vet was not reached.
This remains a failed qualification, not a stable concurrency/performance pass.

Local full race on the same implementation passed Agent 210.580s, Store
16.520s and Hub 3.110s. This difference is retained explicitly. The next
optimization must address serialized concurrent admission and repeated
authority/rate persistence without moving the deadline or bypassing revocation.

Native diagnostic ec07301 logs and b2bdd42 trial/attack/full-suite evidence were
downloaded, archive hash verified against remote:
9e7b85d736a6acc22a1889f062d022d9a82478904d34baa6fbd9190d9834cfbd.
04-22 manifest records all raw hashes. D-04, complete hostile coverage and
formal Phase 4 qualification remain open.

## Single-transaction distinct-authority validation

Existing-block admission now checks each distinct current/held credit grant and
runtime/share lease once in its transaction. The initial joined query already
pins session state and IDs; no recursive re-read of that session or of both
leases is needed for each grant. The validated share record is passed to a
private same-transaction rate helper. No cross-transaction cache or revocation
relaxation is introduced. Ordinary reservation callers still use the original
rate helper, which performs its own share validation.

Full Store race PASS 14.830s; Agent funding and real block renewal race PASS
31.045s. Added distinct-grant renewal regression revokes held grant, current
grant, current runtime and current share separately; every next admission fails
without changing rate tokens (targeted race PASS 3.598s). Vet/diff PASS. Native
concurrency verification is still required; the prior failure remains open.

## Native distinct-authority concurrency test: 0/20 pass

Clean f64d74ac6e537045ba057a07fe104c6797d2c963 on the dedicated host failed
all 20 repetitions of TestFundingBlockGateConcurrentShortWritesAndClose,
17.184s total. Query deduplication alone does not meet concurrent admission
latency. Logs include admissions returning after 536ms while queued on the
ordinary mutex, despite their 200ms context budget. Archive verified:
a5df879475660a268565f292341f08944079aba813827cad988a8f62eae3fb55.
04-22 manifest retains raw hashes; this failure remains open.

Allocator ownership now uses a context-aware mutex for admission. Canceled
queue waits return without changing funds or faulting unrelated requests.
Settlement/close still acquire ownership without dropping liabilities. A test
holds the allocator locked until a 20ms admission cancels, then proves another
valid admission succeeds and all funds reconcile. Funding-gate race PASS
3.117s; Agent/Hub vet and diff PASS. This corrects cancellation semantics, not
the underlying serialized FULL-commit throughput. Bounded batch admission and
rate/checkpoint persistence design remain required before native concurrency
and D-04 can be claimed.

After the cancellation change, real ordinary/delayed TCP/UDP block renewal
race PASS locally 29.977s. Native cancellation and full regression remain
unrun for this final change; no native concurrency closure is claimed.

## Bounded Store batch admission primitive

AdmitRuntimeFundingBlockBatch now validates up to 64 writes and commits shared
rate debits in one transaction. Exact block/scope matches coalesce; their sum
cannot exceed the persisted residual. Time intervals are conservatively united
(earliest minimum, latest maximum), so batching cannot extend validity or
create extra refill. Different scopes sharing a batch identity are rejected.
A failure in any group rolls back all preceding rate updates. No permission
exists until the batch transaction returns successfully. Exclusive allocator
ownership and existing in-flight bounds remain the caller's responsibility.

Targeted batch+admission race PASS 4.537s; full Store race PASS 15.186s; Agent/Hub
vet and diff PASS. Tests cover 16 coalesced writes, exact rate exhaustion,
cross-direction rollback on later denial, aggregate residual overflow, scope
and identity mutation, expired/uncertain time, 65-entry limit and cancellation.
The single-write API delegates to the same transactional validation helper.

No runtime queue invokes this API yet. Agent queue integration must preserve
per-request deadlines, bounds, cancellation and unknown-commit behavior before
re-running the failed native 16-concurrent-write test. This is persistence
implementation progress, not concurrency qualification or a D-04 parameter lock.

## Agent bounded admission queue integration

Experimental block sessions now enqueue at most 64 requests with the 200 ms
deadline anchored at enqueue. A one-millisecond experimental grouping window
is included in that deadline. Contiguous same-direction requests fitting an
existing residual use Store batch admission. New funding keeps its atomic
first-write path. Every child still commits its usage independently; no
settlement ACK is deferred until unrelated children complete.

Grouped transactions inherit the earliest deadline and are canceled if any
member cancels. Successful admission racing caller cancellation returns its
never-delivered child allocation; spent rate is not refunded. Unknown storage
failures fault the gate rather than silently retrying. Close and previous
in-flight/unknown bounds retain the existing mutex serialization.

Funding/queue tests repeated three times PASS 6.939s. Added direct grouped
admission regression proves independent first-child durable checkpoint, short
writes, returns and later unknown residual conservation. Real ordinary/delayed
TCP/UDP block renewal race PASS 29.820s; vet/diff PASS. Native concurrency and
full regression remain required; one-millisecond/64-request bounds are not
D-04 parameter selections.

## Native queue concurrency test 0df7bb1 — still failed

Clean 0df7bb1896b599351f83cec9a334783890d06591 ran 20 repetitions of
TestFundingBlockGateConcurrentShortWritesAndClose on the dedicated host.
All 20 failed (0 pass), total 12.624s, with admission context deadlines.
Raw logs, SHA, clean status and exit code remain remotely under
artifacts/phase4/concurrency-0df7bb1. This is not a qualified throughput gain:
the 200 ms requirement remains unmet. No later full-suite run was started.

Grouping admission alone leaves per-child synchronous checkpoints and small
block-capacity waves serialized. Further work must group durable checkpoints
without acknowledging a child before its used bytes and residual are durably
committed. Simply returning success for memory-only consumption would violate
the existing accounting.Batch resolver boundary and is not an acceptable fix.
The queue implementation remains experimental; D-04 and native concurrency
qualification remain open.

## Ordered group checkpoint transaction

Added CheckpointRuntimeFundingBlocks for at most 64 known outcomes per SQLite
transaction, preserving independent child identities/payloads and ordered prior
residuals. Existing single-checkpoint logic is shared inside the transaction.
A successful return occurs only after every child outbox, funding split and
demand-consumption observation commits. Any failure rolls the whole group back.

Tests inject failure in the second child's outbox insert and prove no first-
child usage or partial residual persists. Successful exact replay is stable,
changed payload is rejected, and close/reopen freezes only the 700-byte residual
after two known checkpoints totaling 300 bytes. Exact replay after that recovery
still preserves conservation. Invalid residual order, duplicate identity,
overflow, over-64 group and canceled context all leave original funding intact.
Targeted race PASS 3.007s; full Store race PASS 16.924s; Agent/Hub vet and diff
PASS. These are Store transaction tests, not process-kill or native performance
qualification. Agent settlement queue integration remains required; runtime
currently still invokes individual checkpoints. D-04 stays open.

## Agent grouped durable settlement queue

Known positive write outcomes use a separate bounded settlement worker (64
pending callbacks, one-millisecond experimental grouping window). It builds
ordered residual checkpoints and acknowledges each callback only after the
shared Store transaction commits. Admission capacity waits and settlement
processing use separate queues. Unknown/returned outcomes retain direct
serialized handling; canceled/uncertain groups poison participating residuals
and fault admission rather than refunding their unknown upper bounds.

Funding/settlement tests repeated three times PASS 6.794s. Added canceled-group
regression proves zero partial known usage, the full 128-byte recovery bound,
no refund on close and refusal of further admission. Existing independent
settlement and unknown-tail tests still pass. Real ordinary/delayed TCP/UDP
block renewal race PASS 30.542s; Agent/Hub vet and diff PASS. Native concurrent
validation and full regression remain required; D-04 is not locked.

## Native grouped-settlement attempt ac5e0e5 — still failed

Clean ac5e0e5557ccef7b8abb06c15985c2ccbf15a4bf on the dedicated host ran
20 repetitions of the original 16-concurrent short-write test. All 20 failed
with context deadlines, total 12.953s. Raw SHA/status/log/exit artifacts are in
artifacts/phase4/concurrency-ac5e0e5 on the host. This does not establish any
qualified performance improvement, and native full regression was not started.

Admission and settlement grouping alone have not met the gate. Next work must
measure queue wait, block funding, grouped admission, checkpoint preparation
and transaction/commit time separately before further structural tuning. The
128-byte fixture block forces several capacity waves; it must not be enlarged
merely to make the test pass without measuring and preserving its requirements.
D-04 parameter selection and full qualification remain unachieved.


## Native profiling and unchanged-deadline non-race control

Owner reiterated approval of control 1.3.0; its existing recorded contract
approval remains applicable. No additional wire extension is approved here.

On dedicated native Ubuntu, ac5e0e5 ran the same 16-concurrent short-write test
20 times without race instrumentation: 20 PASS, exit 0, total 3.183s. The
128-byte funding fixture and 200ms admission deadline were unchanged. Remote
raw log: artifacts/phase4/profile-ac5e0e5/plain/test.log; SHA-256
152f64db68045cce0630406207298ed5a1dc4342119c2df1063d9038f71aec1a.

The separately profiled race run still failed (13.592s). CPU samples include
substantial race/checkptr overhead; SQLite prepare/parser paths account for
roughly 46% cumulative CPU. Profile symbolization warned that its original
temporary binary path was missing, so names may be incomplete. This establishes
an instrumentation confound, not proof that SQLite FULL commits alone caused
the deadline failures. Cumulative CPU percentages are not transaction latency.

Non-race success does not supersede failed race evidence, full attack coverage,
sustained durable-authority performance, or D-04 candidate qualification. Keep
the production deadline unchanged. Further work must distinguish race safety
and production latency, retain raw profiles, and measure transaction phases
before additional tuning. D-04 and Phase 4 remain open.

## Unknown outcome overtakes queued known checkpoint

Added a deterministic worker-boundary regression for two admitted children of
one funding block: an unknown outcome freezes the full 128-byte residual before
the other child's known checkpoint reaches its transaction. The delayed group
must be denied, clear its in-flight count exactly once, retain all 128 bytes as
unknown, record no overlapping committed consumption, refund nothing on close,
and refuse subsequent admission. This complements the existing known-before-
unknown ordering. It tests the worker boundary directly, not OS scheduling or
process-kill recovery.

New ordering, canceled-group, and independent-settlement race tests repeated
three times PASS (2.711s). Original production deadlines remain unchanged.
The dedicated host full uncached non-race Agent/Hub regression is running at
ac5e0e5; its result is not yet claimed here. Symbolization of the earlier CPU
profile was repaired using the preserved matching agent.test binary; checkptr
and race overhead remain evident. No D-04 parameter or phase completion claimed.

## Native full non-race regression completed

Dedicated Ubuntu at clean runtime SHA
ac5e0e5557ccef7b8abb06c15985c2ccbf15a4bf completed
`go test ./agent/... ./hub/... -count=1` with exit 0: Agent 202.891s,
identity 0.030s, Store 8.207s, upgrade 0.241s, Hub 0.212s. Raw remote log
artifacts/phase4/plain-full-ac5e0e5/test.log SHA-256:
066c533d8fd61b081ee153361ce47b5819c216ad14b8b7b85d6c123d2b9fad5a.
This is uncached native functional evidence without race instrumentation. It
excludes the later test-only de71f90 addition; that addition has separate local
race evidence above. Prior native race deadline failures remain failures.
Neither this suite nor process-local d04_run.py establishes sustained durable
Hub issuance performance or completes the D-04 parameter scan.

## Native funding-block disk exhaustion and checkpoint process death

Clean ac5e0e5557ccef7b8abb06c15985c2ccbf15a4bf ran both ordinary and delayed
funding-block durable renewal tests on the dedicated Ubuntu host with
VORTEX_TEST_FULL_DISK_DIR pointing to a separate 8 MiB tmpfs. TCP and UDP
passed, total 27.354s, exit 0. The fixture verifies SQLITE_FULL, refusal of new
Hub issuance, stopping target writes and refusal to reopen the faulted gate
after space returns. This is actual filesystem exhaustion, not physical disk
power-loss testing. The isolated mount was successfully unmounted (exit 0).
Remote artifacts: artifacts/phase4/block-full-disk-ac5e0e5; test.log SHA-256
86a0410086fa4769eae65487a5fcc2ef1262a2320e33c802b37545602bf46ddf.

Added subprocess SIGKILL tests for the grouped checkpoint path. Before commit,
both child mutations have executed in the transaction but remain uncommitted;
recovery must retain 1000 bytes and no child outbox rows. After acknowledged
commit, recovery must retain only 700 bytes with two durable children totaling
300 bytes. Both branches freeze the residual as unknown, preserve conservation,
and check that exact committed replay succeeds while replay of the rolled-back
checkpoint cannot spend the frozen block. Three local race repetitions PASS
3.456s alongside the existing transactional replay/recovery test. Native execution
of this new test remains pending; this is process death, not power loss.

Inspection confirms the existing runtime renewal fixture waits for issuance
before each exchange and idle settlement afterward. It cannot serve as sustained
load or prove demand-wait tail latency. A continuous durable-authority workload
is still required before D-04 candidate lock; current values remain experimental.

## Native checkpoint crash pass; continuous traffic exposes renewal gap

Dedicated Ubuntu clean e78964f7dc69ea542d67146ed60c2c85cd5cdbff ran the
SIGKILL checkpoint and transactional replay tests with race, three repetitions:
PASS 7.078s, exit 0. Remote artifacts artifacts/phase4/block-crash-e78964f,
test.log SHA-256 8a7241d2823fe52160cc54c660b8b2f6e8b57f305d953cae38c1a5284abec3d1.
An earlier fetch used a nonexistent bundle branch reference (bundle exported
HEAD only); the old-SHA attempt is preserved separately at
artifacts/phase4/block-crash-wrong-sha-ac5e0e5 and is not new-test evidence.

Added TestRuntimeFundingBlockContinuousTraffic: authenticated demand consumer
readiness is awaited once, then sixteen exchanges run on one real TCP/UDP
stream without waiting for grants or idle-tail settlement between exchanges.
The existing fixture's durable issuer, lost-response replay, original expiry,
200ms admission bound, final ledger checks and 2048-byte blocks are retained.
It is a functional continuous-demand regression, not sustained NFR load.

The new regression currently FAILS on both TCP and UDP around the 200ms bound.
A TCP failure diagnostic showed Agent Initial=15288, Remaining=3288,
Committed=12000, Reserved=Unknown=0; valid scoped Available=2192;
Hub Issued=15288; no pending demands. Store selection requires a single grant
with at least the 1500-byte write, while renewal compares summed available
credit to the 1228-byte waterline. Evidence points to fragmentation causing
renewal starvation despite adequate aggregate credit; per-grant confirmation
and a bounded, scope-safe fix remain required. Do not weaken the deadline or
hide this failing test. Local raw logs retained under
artifacts/phase4/continuous-regression. D-04 remains unlocked.

## Confirmed fragmented credit; bounded fragment admission primitive

ReadDemandLedger now additionally reports LargestGrant, calculated only after
all existing scope/time/authority filters. It does not change summed Available
or renewal policy. The continuous TCP failure reproduced with Available=1644,
LargestGrant=1096 and a 1500-byte write, no pending demands, and no unknown or
reserved balance. A deterministic Store fixture independently proves two valid
1096-byte grants sum to 2192 yet cannot satisfy the single-grant reservation.

Added one-shot ReserveRuntimeUsageUpTo (1..4 MiB requested), reusing session,
lineage, lease, connection, rate and durable reservation checks. It admits at
least one byte up to the available grant tail and returns the exact grant and
amount. Reusing its allocation identity is rejected; ambiguous errors must be
recovered, never treated as fresh permission. No funds are created and no
renewal threshold is changed. Fragment collection/rollback and whole-write
settlement must still be implemented in Agent before this can fix traffic.

Targeted Store race tests PASS 3.794s, including fragmented reservation, exact
identity, one-shot replay rejection, short-write unused returns and preservation
of a large preexisting unknown liability. The continuous regression remains
expected to fail until allocator integration is complete. D-04 stays unlocked.
Full Store race regression also PASS 16.353s; this does not supersede the failing
continuous runtime regression or establish native evidence for the new API.

## Agent composite admission consumes fragmented credit

Experimental funding-block allocation now falls back after whole-grant denial
to collecting up to 64 ordinary durable per-grant fragments, with at most 64
unsettled composite writes per session. Allocator ownership and the original
200ms enqueue deadline cover collection. No capability is returned until the
entire write is funded. Each fragment retains its grant identity and spends
shared rate through the existing transactional checks. Admission failure returns
only previously confirmed unused children; uncertain current holds remain for
recovery. Known-idle opposite tails are returned before collection; live tails
remain protected. No extra authority is issued to hide fragmented balances.

Settlement partitions known writes in reservation order, returns proven-unused
bytes, and preserves per-child durable outbox identities. Unknown results fault
the gate and freeze all children. A settlement failure retains its uncertain
hold and freezes later children rather than refunding them. Close does not
refund outstanding composite writes. Tests cover two-grant short writes after
close, unknown outcomes, canceled settlement and partial collection rollback.
Three local race repetitions PASS 3.140s; vet and diff checks PASS.

Continuous real TCP/UDP traffic now completes sixteen exchanges without waiting
for renewal balances or idle settlement. Its previous fixed batch-count check
was replaced for this workload with exact Agent/Hub/backend identity and payload
comparison, protobuf byte-total reconciliation, settled-state checks and SQLite
integrity checks. Three non-race repetitions PASS 31.938s. Initial integration
failed only the old batch-count assumption; raw attempts are preserved in
artifacts/phase4/fragment-integration.

The broader local funding-block race run still FAILS the new continuous test
at the unchanged deadline (43.101s overall). Its diagnostic had Available=4096,
LargestGrant=3000 for a 1500-byte write: the earlier fragmentation deadlock is
resolved, but instrumentation/latency qualification is not. Do not count this
as race pass. Native composite validation, crash/fault coverage of collection
and sustained multi-stream D-04 scans remain required. No parameters locked.

## Native composite admission validation a128471

Dedicated Ubuntu clean a1284717a194fc86d77d7e25db339283d3c4763a ran
continuous real TCP/UDP traffic three times: PASS 35.137s, exit 0. Each run
uses unchanged 200ms admission deadlines and exact three-ledger reconciliation.
Separate three-repeat native race tests for composite settlement outcomes,
fragment identity/unknown conservation, and existing grouped checkpoint SIGKILL
recovery also passed (exit 0; Store 6.381s). Raw artifacts:
artifacts/phase4/fragments-a128471 on the dedicated host.
continuous.log SHA-256 e3b003f83ff1740f7374a9089424ba2817f6eb2aa444ade38c26bfbcd143cd87;
fault-race.log SHA-256 8cf29304b4654b03c9fc59c05a1fd980d2eb2d9c15ba6f1ce981a3f7695c3b1c.

Added three real child-process SIGKILL cases for ordinary fragment collection:
after the first 100-byte hold, after two 100-byte holds, and after committing
70 bytes from the first hold. Reopening must retain respectively 100, 200 and
100 unknown bytes; only the last case has 70 committed bytes and a proven
30-byte return. Source conservation is asserted after recovery. This directly
tests durable transaction boundaries, not physical power loss or Agent process
startup. Local race plus fragment identity tests repeated three times PASS
3.410s. New subprocess cases still need native execution.

Native full non-race Agent/Hub regression and vet were launched at a128471;
results pending. Existing continuous race timeouts remain unresolved. These
functional successes do not constitute sustained multi-stream D-04 qualification.

## Native full composite regression and binary block-mode entry

Dedicated host full uncached `go test ./agent/... ./hub/... -count=1` at clean
a1284717a194fc86d77d7e25db339283d3c4763a PASS: Agent 219.089s, Store 8.280s,
identity 0.027s, upgrade 0.248s, Hub 0.232s. Vet exit 0. Remote full.log SHA-256
a5fe732642ffb9232a47f469fad93dfc2655f43e109fea622d0d0e5391dbde9a,
vet.log SHA-256 e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855.
This native evidence excludes later test/CLI changes and is non-race.

The node binary now supports explicit --experimental-funding-block-bytes, default
zero preserving per-write mode. Node.EnableExperimentalFundingBlocks derives
scope from its own loaded RuntimeEngine, uses its trusted clock and refuses
non-runtime nodes, zero/over-4MiB API sizes and replacement after Start. Existing
real block-runtime tests now use this same startup entry. Continuous TCP/UDP
plus size/live-replacement boundary checks PASS locally 14.049s; vet/diff PASS.
No authority is granted by this option and no wire schema changed. README makes
clear that current d04_run.py does not yet drive this durable process entry.
Multi-process sustained workload and parameter lock remain outstanding.

## Protocol-test Hub binary durable authority fixture

Added explicit paired --authority-fixture / --fixture-backend-db options to the
existing protocol-test Hub. Strict single-object JSON input is capped at 1 MiB;
caller supplies finite delegation, rate/share, absolute time boundaries and a
stable initial request identity. Boot identity binds only at the authenticated
control callback; wrong-node and different-boot replay are rejected by fixture
and immutable Store checks. TLS Hub identity must match the fixture, and the
backend cannot alias the Hub database. Initial issuance and adaptive demands
use the existing durable issuer; known/unknown usage uses a separate durable
protocol-backend adapter, returning its digest only after persistence.

Three local race repetitions PASS 2.202s: persisted initial issue survives
reopening without another debit, wrong node/new boot rejected, and known usage
replay succeeds through durable backend confirmation. Vet/diff PASS. This is
fixture wiring, not yet independent-process E2E evidence. Fixed heartbeat and
autonomy boundaries remain enforced; no fake always-online heartbeat or implicit
budget renewal was introduced. Full process orchestration and sustained workload
metrics remain required, as do native tests of this new entry. D-04 remains open.

## Independent Node / test-Hub TCP functional harness

Added tools/phase4/durable-process.sh and an explicit binary-driven integration
test. The real vortex-node and vortex-test-hub run as separate child processes;
source VLink driver and echo target remain in the test process. Fresh control
mTLS credentials, finite delegation, experimental 2048-byte blocks and adaptive
demand configuration drive sixteen 1500-byte TCP exchanges. No direct Agent
ledger provisioning occurs. Shutdown waits for exact 48000-byte durable ACKed
usage; all three ledgers are then checked for exact identity/payload, byte total,
settled state and SQLite integrity. Evidence retains logs/configs/databases,
commit/dirty state and binary hashes, with performance_qualified=false.

First local attempt failed reconciliation at 46500 bytes after the driver reset
the stream immediately after receiving the last response. This exposed the
required asynchronous settlement drain in the driver. Corrected attempt waits
for exact durable ACKed usage before reset and passed. Original failure retained
at /private/tmp/vortex-process-first; corrected evidence at
/private/tmp/vortex-process-drained. These are local dirty-tree development
attempts, not native or clean-SHA qualification. Shell syntax, vet and diff PASS.
The harness uses preissued valid certificates with a renewal endpoint configured
but does not exercise renewal; fixed Hub heartbeat/autonomy limits still apply.

Clean local process rerun PASS at b8a10dc3f9e7cf5044a80dfb87b28073695c6688 (dirty=false).
Report: /private/tmp/vortex-process-clean/report.json; SHA-256 e7ffe66c8c982a503dbad479a3635fb9e6454cbc25a7d75a44b425600e2c647f.
This confirms only the two-process TCP functional scope described above.

## Native process TCP pass and UDP functional expansion

Dedicated Ubuntu clean e7ade792de46e0bbdd6b861c7857b5d6c0655b62 executed the
independent Node/test-Hub TCP check: PASS 10.266s, dirty=false. Remote evidence
artifacts/phase4/process-e7ade79. report.json SHA-256
facde098a621fb0c0007c868275ed33188555c28be07d8cc5e7334b86bbcfefa;
test.log SHA-256 e9e9acd746d11a4096827fe91fa670af3ff788d23c5a99ec4f543cdbd42ea197.
Scope excludes later UDP and source-audit additions.

Expanded the same process harness to independent UDP target sockets and VLink
datagrams, requiring intact per-datagram boundaries. Both protocol cases now
also reopen stopped ledgers to assert exact 48000 committed bytes, zero reserved
and unknown balances, per-source conservation, and exact Hub-issued/Agent-
received credit equality. All three payload ledgers remain compared exactly.
Local development run passed both cases at /private/tmp/vortex-process-tcpudp-
conserved (actual directory name has no newline). Report generation now requires
explicit PASS markers for both cases; missing/skipped tests fail closed.
This is functional evidence, not UDP offered-load or sustained qualification.

Clean local TCP+UDP process rerun PASS at 711dbb3109d18d17dcb8b7b1b9df9681c1824b1b; both explicit test markers verified. Report SHA-256 0d299516971b1e79951df37197308da3454d9f9d82d37f53f04b41c178735ebb. Evidence: /private/tmp/vortex-process-protocols-clean. Native UDP remains pending.

## Native UDP process failure and concurrent candidate fixture

Dedicated clean c5eebf0e2efd4f99902628e542ade67cd1cd73ce process run passed TCP
but failed UDP with FrameRST (type 7). Overall report correctly failed; raw
artifacts remain at artifacts/phase4/process-c5eebf0. The stopped UDP Agent DB
had 25 committed usage reservations and two residual reservations (1096 and
144 bytes); this is not a successful UDP or source-settlement verdict. Initial
node log did not contain an explicit admission error. Investigation continues;
local success cannot override this native failure.

Added an independent-process 16-concurrent-stream TCP case with explicit
256 KiB blocks, 8 MiB cold-start/target, 64 MiB delegation and 100 Mbps share
rate. Each stream performs sixteen 1500-byte request/response exchanges using
its own payload pattern; exact total is 768000 bidirectional bytes. This is a
candidate functional configuration, not a locked default or sustained NFR run.
All three local process cases passed in the development metrics run at
/private/tmp/vortex-process-concurrent-metrics. Per-case workload.json records
parameters and traffic.json records actual bytes/time plus p50/p95/p99 round-
trip samples, explicitly excluding the later ACK drain. Report now requires all
three named tests. These metrics are not issuer-wait latency or UDP offered-load
qualification, and the concurrent case has not run natively yet.

Failure teardown now gracefully stops Node then Hub before canceling their parent
context, preserving final logs instead of immediately killing them on t.Fatal.
Unexpected frames report stream, round, type and payload before buffer release,
to diagnose the unresolved native UDP reset on the next clean-SHA attempt.

## Native b9fc9b2 concurrency failure and syscall diagnostic

Clean b9fc9b211954d4b66a1fc0471f72573d5d37cc95 dedicated-host process run
passed single-stream TCP and UDP, but 16-stream TCP failed at stream 7 round 0
with RST payload 0008. Node logs show admission reaching approximately 199ms
at begin-transaction (and one at reserve-usage). Overall remains FAIL. Report
SHA-256 95f790b1ed5604718c6fbc2ab3aae9889c416756d552f4d357019cd8fed83edc;
test.log SHA-256 641cde3b2beacc7c84799738cb2c85aee38fd8203726f982ac5943b90b6fa6a7.
Artifacts: artifacts/phase4/process-b9fc9b2. This run's UDP pass does not explain
or erase the previous UDP reset. Waiting for the single Store connection is
confirmed; attributing that wait exclusively to FULL fsync is not justified.

A diagnostic strace -D -f -c run over the same node binary failed concurrency
again (0.494s). Selected syscall accounting: 161 fsync calls, 0.005613s summed
syscall time (34us average); 1714 pwrite64, 0.009766s; 1032 fcntl, 0.008974s.
These are traced syscall timings, not wall latency distributions or proof of
power-loss durability. They do not support claiming fsync alone accounts for
the 200ms wait. CPU/SQL processing and serialization still need profiling.
Artifacts: artifacts/phase4/transaction-trace-detached-b9fc9b2.
The earlier non-detached tracing wrapper interfered with shutdown and produced
no usable summary; its surviving test node PID 127348 was explicitly terminated
and confirmed absent before repeating. That failed diagnostic is preserved at
artifacts/phase4/transaction-trace-b9fc9b2, never counted as qualification.
No production deadline, SQLite FULL setting or D-04 candidate was changed.

## Cross-session queue diagnosis and explicit profiling entry

Inspection confirms admission coalescing is per fundingBlockSession. Sixteen
separate streams cannot share their initial allocation through that queue;
increasing its one-millisecond window is therefore not an evidenced fix and
was not applied. The native begin-transaction delay remains unresolved.

Added explicit node --diagnostics-dir for CPU, block and mutex profiles. It
requires a fresh local directory, exposes no network endpoint, and writes only
on opt-in. Graceful shutdown flushes profiles; fatal os.Exit or SIGKILL may leave
incomplete evidence and must not be treated as successful capture. Profiling
adds overhead and is diagnostic only. Default runtime parameters and SQLite FULL
remain unchanged. CLI tests cover disabled mode, fresh-directory capture and
refusal to overwrite prior evidence. Native profile capture is the next step.

## Native CPU/block capture and same-transaction read deduplication

Clean c18eadac04291f0276dc80154aefbd988e076df6 dedicated-host instrumented
16-stream process run failed again (0.429s) and gracefully emitted all profiles.
Artifacts: artifacts/phase4/profile-c18eada, including matching node-real binary.
CPU capture duration 379.94ms, 400ms samples; Tx queries account for 170ms
(42.5%), initial funding reservation path 150ms (37.5%). Block capture shows
DB.conn 3.37s cumulative wait and BeginTx 3.25s; these are aggregate goroutine
waits, not single-operation latency. Startup/shutdown are included, and short
sampling means these percentages must not be treated as precise steady-state
attribution. CPU SHA-256 aec68704f84c51babbd4de4354b52a286390fe1b99de39b375440daf3011e482;
block 5f2e4baad23cf61b8057395635b2dc1993edd1646cca5106953bd40234229e52;
mutex 4765f79a47505d1595f2949e9d401eb72582ab52b1883381da0300e8ed126bfb.

Removed one redundant authorization-record query per examined candidate:
selectRuntimeAuthorityTx already reads the record and state, so it now passes
that same-transaction value to a private validation helper. All original scope,
trusted-time, state, lineage, deadline, lease and session checks remain; no
cross-transaction authority cache exists. Ordinary callers still read then
validate normally. Full Store race PASS 17.353s. This is a bounded query reduction,
not proof the native deadline is fixed; clean native retest remains required.

## Clean 417ce63 native retest

User reiterated approval of control 1.3.0; this does not approve control 1.4.0
grant return. Continued the existing 04-12 execution on the isolated branch.
Clean 417ce6384a3154af46a46212814ef114cfcea9cc was deployed to the dedicated
Ubuntu host and tested without profiling using durable-process.sh.
Artifacts: artifacts/phase4/process-417ce63. Overall FAIL (11.644s): UDP PASS
(10.27s), single TCP FAIL at round 9 with RST 0008 (0.97s), concurrent TCP FAIL
at stream 11 round 0 with RST 0008 (0.40s). Concurrent admission logs again show
begin-transaction timeout at approximately 199ms. Single TCP logs show a
select-authority deadline after 198ms total admission time. Query deduplication
has not resolved the qualification blocker. Do not infer regression causality
from one run; intermittent single-stream resets existed before this change.
Report SHA-256 e02a1b2b5f7c174ee383fc45c6f20da4cadfb3b501bb3065ba94268603c9d438;
test.log SHA-256 d1f2a9c230a4471e22caf44c95492d66e1eec267b900b7103f75bb40a9b1870b.
No timeout, durability setting or candidate default was relaxed. D-04 parameter
lock and full Phase 4 closure remain unverified. Next work must address measured
cross-session transaction contention and retain per-write authority and durable
settlement guarantees, rather than increase the deadline to pass this test.

## Sustained native SQL profile and fragment crash coverage

Read-only inspection of the failed concurrent database found only 3 authorization
records, 16 sessions, 22 funding blocks and 36 reservations. Large authorization
history is not supported as the explanation for this failure.

On the same clean 417ce63 host, ran BenchmarkRuntimeDurableAccounting/bytes-1200
with -benchtime=5s and CPU profiling. PASS: 934 iterations, 6,825,069 ns/op,
72,794 B/op, 2,290 allocations/op. These are instrumented ordinary per-write
reservation/settlement measurements, not block throughput, network throughput,
or qualification. Profile duration 7.18s with 8.14s CPU samples: SQLite prepareV2
3.45s (42.38%), parser 3.36s (41.28%), readAuthorizationTx cumulative 2.38s
(29.24%). Nested percentages overlap and must not be added. This strengthens
the case to measure prepared statement reuse before redesigning admission queues.
The installed modernc sqlite v1.61.1 stmt.go caches a compiled single-statement
handle for a retained driver statement; ordinary QueryContext constructs and
closes statements. Reuse must retain transaction isolation and fresh row reads,
not cache authorization results. Benefits on block concurrency remain unproven.

Also ran TestRuntimeFragmentSIGKILLRecovery with -race -count=3 on the dedicated
host: all first/all/settled-first crash boundaries PASS, total 6.838s. This closes
the missing native evidence for the existing fragment crash test, not the full
attack matrix or current-version disk-full coverage.

Artifacts: artifacts/phase4/store-profile-417ce63 (matching store.test included).
SHA-256 benchmark.log ed4b712d913a2601153c74311c6cbd0d068d7f54898cd92f45fc0a00e775c2a1;
cpu.pprof 7653ebf07057f38288aa54b6a7e4ad3447319930e4fb13c53d83be6110e28290;
fragment-crash.log d515b6746dc1a965a8fb8a87004b7dfbb0fd5b9438b3915026621584cb27085b.

## Prepared authorization query reuse

Implemented Store-owned prepared authorization record reads bound to each runtime
transaction with sql.Tx.StmtContext. Only compiled SQL is retained; every call
reads the transaction's current rows. The single connection, WAL/FULL, admission
deadline and authorization predicates are unchanged. Internal helper signatures
accept the common transaction SQL interface, so ordinary sql.Tx callers keep
their existing behavior. The parent statement is prepared after schema setup
before any transaction acquires the single connection, and closes with the DB.
Regression checks cover own-transaction revocation, rollback/commit visibility
across transactions, and cancelled read followed by successful fresh reuse.
Local full Store race PASS 19.145s; Agent/Hub vet and diff checks PASS. Dedicated
host performance and process comparisons still required before claiming benefit.

Clean 4e7d849b277cc5cb01892f6f5319e4d281447bf4 dedicated-host retest:
single TCP PASS 10.25s, UDP PASS 10.27s, concurrent TCP FAIL 0.39s at stream 9
round 0 (RST 0008), overall FAIL 20.921s. First logged admission failure again
waits approximately 199ms at begin-transaction. Artifact process-4e7d849 report
SHA-256 583141191c348b51b67a55244f63f99640dc6d8eed471e24576766a93cb6f863;
test.log b4bdb68df0f1360d5c4b9cdf35ac130e2ba8517e161a1c90a11a7e4b7fbc4526.

Sequential (not concurrent with process tests) matching instrumented 5s ordinary
1200-byte benchmark PASS: 907 iterations, 6,142,366 ns/op, 79,416 B/op, 2,413
allocations/op. Prior single sample was 6,825,069 ns/op; this is not repeated
statistical evidence of improvement, and allocations increased. Profile 6.42s,
7.12s CPU samples: prepareV2 2.54s (35.67%). Remaining SQL preparation is still
substantial; caching one read has not solved concurrent qualification. Keep both
results. Artifacts store-profile-4e7d849, benchmark.log SHA-256
43ba0c1e23e0cab1a195ed88ff8c179a145da3f9ed9ff4f305b35e1854eba272;
cpu.pprof 7dc288bead3650f4240b933761fae4b81381dd84b37ce4b0b0105988ecfaaf29.
Local Hub and node/test-hub CLI tests also PASS. No D-04 default is locked.

## Bounded runtime statement reuse

Expanded prepared SQL reuse to a fixed 47-statement allowlist covering runtime
admission, reservation and settlement reads/writes. Original SQL remains at call
sites; nonmatching SQL falls back to ordinary transaction execution. Parent
statements are prepared after schema setup, transaction bindings are created
lazily and retained only for that transaction. A mutex protects binding creation;
cancelled binding attempts are not retained. All parameter values and row reads
remain per operation. No grant, lease, rate or balance result is cached.
Ordinary settlement now also uses the runtime transaction wrapper. Added same-
transaction read-after-cancellation coverage. Full Store race PASS 20.508s;
Agent/Hub vet PASS. Native process and benchmark comparison remain required.

Clean 4b19614bc215dfd3f1fbee50e5e93577db351103 native run: single TCP and UDP
PASS; 16-stream TCP FAIL at stream 14 round 1 with RST 0008 (0.49s), overall
21.032s FAIL. The first admission deadline log reports only 10.278ms in
reserveDirect and 10.195ms at begin-transaction. Reserve's 200ms deadline begins
at enqueue, before reserveDirect; this log cannot attribute the full request
delay to SQL. Next diagnosis needs queue/settlement wait segmentation.
Sequential 5s profiled ordinary per-write benchmark PASS: 1482 iterations,
4,046,096 ns/op, 91,221 B/op, 2,549 allocs/op. This single sample is faster than
the prior samples but allocates more; it does not qualify block concurrency or
prove statistically stable improvement. Local Hub and CLI tests PASS.

Artifacts: process-4b19614 and store-profile-4b19614 under artifacts/phase4.
SHA-256 report.json 6d0cd70009ca8eb3173cb39864eb16dab25338e1823130e93ef9119c8a7cc21f;
test.log d856d44397c7e258e6c4a29c2f683808259eb7b1b51c11306f9edb98019b95df;
benchmark.log b4826d90c3eb9b621e0176f322c5df2da6ea9577d04894d1c0628a6b6646ad18;
cpu.pprof 901d7d18ee6db91b383796ad5bdd9d283d3834cbaed20103b02d31b6bf505f30.

## Admission timeline diagnostic

Added failure-only enqueue-to-delivery timing: queue wait, existing-block batch
attempt (including session mutex), and direct admission attempt. Timings are
owned by the queue worker; request delivery remains buffered and no deadline or
authorization behavior changes. Group failures inherit their shared attempt
start. Local funding-block race tests PASS 46.701s; Agent vet PASS. Native
capture is required to attribute the shortened direct-attempt timeout.

Clean ae37aa0a023cb6f741fee8e80be461b1df4bb46a dedicated process run: single
TCP/UDP PASS, concurrent TCP FAIL at stream 14 round 0 (0.44s), overall 20.984s
FAIL. First deadline timeline: total 200.241ms, queue 1.025ms, existing attempt
2.611ms, direct attempt 196.605ms; Store begin-transaction 196.419ms. This first
failure is dominated by DB connection waiting, not the coalescing timer. Other
cancelled requests show existing-attempt waits as high as 141.201ms, consistent
with waiting for session mutex held by settlement; these are secondary failures.
Artifacts process-ae37aa0 report SHA-256
f6ec84a1ca8f854a9ea4e567ec09741c2f7a9b05494deb4857414b15c7036e21;
test.log e315fa50f20cf6d2b5d7622d85ba3be05c96fce45d8b7e2278f5b3ff87e070d9.
Final diagnostic version funding-block race PASS 45.584s.

Removed an unnecessary singleton batch probe: a queue containing one request
cannot coalesce, so it now goes straight to the ordinary direct admission path,
which still acquires the session mutex and performs every authorization check.
This avoids taking the same mutex twice and does not relax synchronization or
deadlines. Funding-block race PASS 45.973s; Agent vet/diff checks PASS. Native
effect remains unmeasured; no claim that DB contention or D-04 is resolved.
