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
