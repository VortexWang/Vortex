# Control 1.4.0 candidate / authorization 1.2.0 / runtime 1.2.0 candidate

The previously approved lifecycle 1.3.0 behavior below remains unchanged. The following additive rule path is pending the exact owner freeze decision; implementation and executable evidence do not constitute that decision.

## Revision-scoped rule transactions (1.4.0 candidate)

The initial authenticated NodeControl Heartbeat may advertise `rule-two-phase-v1` in the new repeated capabilities field (tag 3). The backend selects this known capability only on that current node/boot stream; it sends RuleCommand (ControlMessage tag 18) only to an advertising peer. Old 1.3 peers without this field receive no RuleCommand. The separate Hub LifecycleHello/HubStatus version stays 1.3.0: rule commands use direct backend-to-Agent NodeControl and never acquire Hub financial authority through this extension. Unknown protobuf fields remain subject to each existing consumer's validation policy. Old RuntimeConfigUpdate semantics are unchanged, including its legacy apply sequence; new artifacts must never be tunneled through that message as a substitute for capability negotiation.

RuleCommand phases are `prepare`, `commit`, `query`, `abort`, and `release`; RuleReceipt is NodeMessage tag 19. A transaction is a nonzero 16-byte UUID. Task key and idempotency key are nonempty strings of at most 128 bytes; target revision and fencing generation are positive. Payload is at most 524288 bytes of canonical UTF-8 JSON whose SHA-256 is the exact 32-byte artifact_sha256. Node/boot come from the authenticated envelope/stream and pinned artifact. Every receipt repeats phase, transaction, task key, target revision, idempotency key, artifact digest and fencing generation; mismatch cannot advance durable task or desired revision. The same task key with changed command correlation rejects. Revision, signer epoch, path generation and fencing generation remain independent counters.

Prepare validates the committed immutable artifact and current local authorization, persists its transaction and resource holds in SQLite FULL, and reserves real sockets without activation. Only all prepared receipts for the deployment's fixed member set permit durable Commit task creation. Commit rechecks current authorization/fences before activating. State is `prepared`, `active`, `aborted`, `released`, `failed`, or `unknown`; stable errors remain user-safe. Lost response, disconnect, process restart or unproven execution enters unknown and retains occupation. Query observes the original transaction and executing process identity; it cannot invent a new transaction or infer non-execution from absence. An independent restarted executor cannot attest that the prior executor stopped.

Abort releases only a proven prepared transaction; it cannot terminate an active transaction. Release is a distinct explicit socket-stop operation for the original artifact/transaction. It closes listeners and sessions before replying `released`; only authenticated current node/boot/generation/task/lease-bound proof may free PostgreSQL port occupation. Unknown, failed or stale release responses retain occupation. Pending obsolete tasks may be superseded; in-flight tasks remain queryable. Default 120-second deployment timeout appends a new rollback revision, rechecks current authorization/resources and retains unknown old holds; it does not relabel an old revision as newly active.

Runtime 1.2.0 has mutually exclusive legacy 1.1 and rule-artifact input branches. A rule artifact pins tenant/rule/revision/node/boot, signer/path/fencing counters, immutable billing snapshot, input digest/version map, role, upstream/next member incarnations, selected segment direction and session stability. Canonical field ordering and bytes come from the compiler and strict runtime decoder, not arbitrary JSON key equivalence. The executable path has at most 16 groups, each with at most 256 members; the compiler rejects larger desired inputs before producing artifacts. Mode/path cardinality is direct=1, tunnel=2, multi_hop>=3; ordered group path has no duplicate, targets/members are bounded, port is already allocated before Prepare. Missing immutable grant/member/permit/billing inputs leave compilation pending and emit no artifact. Snapshot producers and source accounting remain later Phase 6 plans; a schema does not mint funded authority. New target sessions recheck the destination policy; existing sessions keep their pinned target/path/billing snapshot through hot updates. Backend and Hub carry control only, and Engine has no database/control client.

The semantic JSON schema bounds shape only. Compiler/runtime semantic validators, canonical byte/digest tests, old-descriptor wire tests, actual TLS forwarding and durable database/process tests establish the additional constraints. Linux target cross-compilation is not native performance or full-system acceptance.

## Previously approved 1.3.0 lifecycle behavior

Authoritative only when these exact bytes and versions have owner approval recorded in governance. A review-directory copy confers no runtime authority. The originating review manifest defines the exact review unit. This document and lifecycle.schema.json / identity-profile.json are normative companion artifacts of control 1.3.0, even though no existing protobuf tag is removed. After approval they must be copied beside contracts/protobuf/v1/vortex.proto and their hashes included in governance validation. vectors.json is non-normative executable review evidence. Protobuf defines binary layout; lifecycle.schema.json defines bounded semantic projections, not ProtoJSON encoding. Protobuf JSON uses its standard uint64 string/base64 rules; tests of projections are not claims of ProtoJSON validation.

## Compatibility and capabilities

Existing NodeControl service/method and every existing field/tag remain unchanged. New NodeMessage tags: lifecycle_hello=16, node_report=17, renewal_request=18. New ControlMessage tags: hub_status=15, renewal_result=16, credential_revocations=17. New HubControl.Connect is a separate backend control adapter; it never carries business TCP/UDP payloads. Existing NodeEvent does not carry new authority operations.

An upgraded node first sends the existing authenticated Heartbeat, then LifecycleHello(control_version=1.3.0, fresh random session_id, configured region, requested capabilities). HubStatus must echo that session_id and correct region, authenticated Hub identity and mutually supported capabilities within readiness timeout. Each side intersects capabilities, never trusts a claimed version alone. Unknown capability labels reject as invalid_request; mutually unsupported known capabilities are not selected. No new lifecycle authority operation before successful negotiation on this stream. Duplicate identical hello is idempotent; changed hello on a stream closes it. Transport session_id is not boot_id or signer_epoch. On reconnect a fresh session_id is required; reusing an old session ID is rejected. Sequence numbers remain positive monotonically increasing in each sender scope, persisted before send where financial intent is present; stop at MaxInt64 rather than wrap. Old streams cannot install config, clock, identity, restriction or authorization changes after their local incarnation is superseded. Already committed usage/unknown receipt proof may be retained idempotently without accepting late authority.

Legacy control 1.2.0 and authorization 1.1.0 continue unchanged when new capabilities are absent. A new node requiring lifecycle features fails readiness against a legacy peer instead of assuming success after ignored protobuf fields. A new Hub may serve legacy clients only with legacy payloads and no new lifecycle features. Authorization schema retains the entire 1.1.0 branch; only the new 1.2.0 branch requires share_references and policy_transitions arrays and per-record share_reference_id/delegation_id. New fields must not be sent to old strict readers. New consumers explicitly select the approved schema version; no downgrade may strip share/policy restrictions and reissue equivalent rights as legacy. Runtime 1.1.0 preserves old valid inputs; lifecycle/probe/share-policy fields are opt-in. Before enabling them all affected receivers must have negotiated support. Older binaries encountering such stored configuration fail closed; auto rollback is blocked unless old-reader compatibility was proved before drain.

## Bounds, parsing and errors

Hard envelope limit 1 MiB before body allocation for new Node/Control/Hub/Backend messages; gRPC receive/send limits must be configured and application payload length checked independently. Existing lower bounds remain effective. Counters/epochs/revisions/amounts are at most 9223372036854775807 to remain lossless in SQLite; sums checked before arithmetic. Raw amounts must be positive. Canonical UUID is lowercase 8-4-4-4-12 hexadecimal; reject whitespace, alternative spellings, empty or duplicate IDs. Bytes SHA-256 fields are exactly 32 bytes on wire, lowercase 64 hex in JSON projections. All strings use valid UTF-8; logical IDs at most 128 UTF-8 bytes (ledger record_id up to257 to preserve original node/batch composite keys), endpoints/server names at most 253. Timestamps are UTC RFC3339 with optional fractional seconds at most nanoseconds, year 0001..9999 and Z suffix. No local-time or floating-second conversion for authority.

Strictly reject unknown/duplicate JSON object keys, null required values, trailing JSON values and unsafe integers; new typed protobuf lifecycle bodies reject nonempty unknown fields recursively after capability negotiation, including unrecognized oneof alternatives. Existing legacy behavior is not silently changed. Max capabilities 6 unique known strings, reports 1024 probe entries with distinct target IDs; share references 4096 per update with max chain depth 32; transitions 64, revocation IDs/serials 4096 unique, regional Hubs 16 unique. Pre-allocation transport cap still applies when an array cannot fit those cardinalities. Backend allocation share path max 32. Large revocation updates are monotonic additive batches: never replace the full revocation set or erase previously revoked IDs merely because absent from a batch. A revocation snapshot reset requires a separately reviewed protocol.

Stable codes: invalid_request, unsupported_capability, identity_mismatch, stale_session, stale_revision, closed_epoch, scope_denied, quota_exhausted, time_uncertain, storage_unavailable, temporarily_unavailable; successful renewal uses ok. Only storage_unavailable / temporarily_unavailable may have retryable=true. Rejects have no authorization payload; ok has exactly one AuthorizationUpdate. RenewalResult.request_id and request_sha256 match outstanding durable intent; projected authorization_update_sha256 is the SHA-256 of the exact AuthorizationUpdate.payload bytes. Correlation fields in authorization 1.2.0 must match the same request. Error details never reveal configured target addresses or secrets. Protocol/identity errors close the stream; a bounded rejected renewal does not release reservations or terminate unrelated valid sessions.

## Reports, readiness and periodic renewal

NodeReport is telemetry: build, applied version, probe summaries, session count and drain state. It never supplies trusted time or a backend heartbeat and cannot authorize, consume, release, rebalance or extend a deadline. Probe targets are references to already applied local rule targets; no remote-supplied target address or arbitrary network scan. UDP probes use a configured supported health response, otherwise report unknown, not healthy after send alone. Counters saturate at thresholds. Default candidates: probe every 3s, timeout <=1s, failure threshold 3/recovery threshold 2, at most 16 concurrent probes; report every 10s. Scheduler uses monotonic elapsed timers; wall-clock timestamps are observational.

HubStatus.mode is online/autonomous/recovering/blocked, authenticated only on the current selected Hub/region/session. Readiness requires the mode be explicitly known and backend authority bounds already verified. An uninitialized Hub may report blocked with signer_epoch=0 and empty backend time/deadline; blocked is never eligible to issue or complete authority readiness. Other modes require positive epoch and valid original times. Prefer an online authorized same-region Hub; do not connect to an unconfigured peer based on its advertisement. A status report is never proof of transferred funds or confirmed ledger. Suggested configurable dial/readiness=5s, watchdog=15s, failover budget=60s, retry jitter exponential 1..30s. A received status cannot extend persisted backend time/deadline; this Hub may only report previously authenticated authority. In recovering, confirmed_sequence can exceed finite recovery_target_sequence as later tail settles; transition online only after target barrier, required revocations/restrictions and fresh authenticated backend heartbeat all commit. pending_tail_records is reported independently.

RenewalRequest covers an existing node/boot/delegation/source/rule/revision/path/fence/epoch/snapshot only. Requested TTL 1..300s, requested_raw_bytes <=MaxInt64. The existing delegated runtime/share values are renewed unchanged; requests cannot choose new rate/connection allocations or targets. Trigger for existing authorization at configurable 30% remaining or remaining TTL <= max(5s, TTL/3), one outstanding request per lineage, minimum retry 1s. All actual expiries remain <= original funding/parent/hard/autonomy boundaries. Health failures or control-switch timeouts never reset them.

Digest: SHA-256(ASCII "vortex.renewal.v1" + byte 0 + deterministic protobuf RenewalRequest with request_sha256 cleared). node_id/boot_id are included in this body and must equal authenticated NodeMessage envelope/session identity. Unknown fields are rejected before hashing. Persist request ID, exact digest/intent and outstanding state in Agent FULL transaction before send. Hub atomically validates lineage, CAS/epoch and available budget, persists the signed/encoded result and budget debit before returning. Same issuer, request ID and same digest replays original result; same ID with changed digest rejects. Lost response retries the SAME ID/digest, including after restart only if original boot remains valid. Fresh boot may query/settle previous intent but cannot turn it into active new-boot authority. A different Hub may not independently issue a retry just because Agent failed over: it must possess the same durable result/ownership handoff proof; otherwise scope_denied/temporarily_unavailable and retain the outstanding liability. Successful result is durably installed before Engine can consume it. Reply/ACK arrival never makes unknown consumption actual or releases it.

## Authenticated backend adapter and durable recovery

HubControl uses TLS 1.3 mTLS with a role-specific backend trust root/configured expected DNS and backend UUID mapping. Node certificates cannot impersonate backend/Hub issuers. First HubMessage is LifecycleHello with session ID bound to the verified Hub UUID/boot/region; BackendHeartbeat echoes session/Hub/region and authenticated configured backend_id. Subsequent backend sequence is monotonic, duplicate identical message ID/body replays, conflicting bytes reject. Persistent authority sequence/freshness floor prevents old-stream/restarted replay. Heartbeat observed_at must advance verified authority time conservatively, never extend a delegation's original hard bounds. Only this authenticated backend adapter (not NodeReport or HubStatus) can update Hub backend heartbeat. After 30s lost backend contact, Hub becomes autonomous; hard cutoff is last successful heartbeat plus configured autonomy max, <=12h candidate ceiling. Restart does not extend it; uncertain time prevents new cross-boundary rights.

BackendAllocation.payload is UTF-8 JSON satisfying lifecycle.schema.json#/\u0024defs/backend_allocation. Allocation ID must match envelope; exact payload SHA-256 matches. Hub/node/boot/source/rule and issuer scope bind authenticated backend delegation. The adapter persists allocation, idempotency key/digest, heartbeat, restrictions/revocations in FULL transactions before exposing issuance. Duplicate ID cannot refill balance; changed payload or closed signer epoch rejects. Higher epoch requires existing unique S/R proof; connection change is insufficient. Runtime/share/credit allocations remain independent conserved sources. Production backend allocation/business policy is Phase 6; Phase 5 fixture supplies finite preinstalled allocations through this same authenticated boundary and is labeled test-only.

Revocations use configured authenticated issuer UUID, increasing revision and effective_at. Apply restrictions/revocations before recoverability or new issue. Future-effective revocations are durable and activate conservatively at Earliest >= effective_at; if uncertainty overlaps effective_at, deny new affected issuance/OPEN until safe. Existing serial/node revocation is monotonic; duplicate revision+same hash idempotent, changed hash rejects; lower revision cannot restore rights. No issuer may revoke/replace another trust domain. Certificate expiry is enforced independently.

LedgerRecord.kind is one of issue/known/unknown/handoff, matching the original durable row. Its payload is the exact original stored bytes: issue/handoff JSON or known/unknown deterministic protobuf (no receiver recanonicalization); record_id maps to its durable row (issue/handoff UUID; known/unknown original node_id + slash + batch_id composite), payload hash and positive sequence match. Payload max 1 MiB envelope; zero-length rejected. LedgerAcknowledgement binds record_id, kind, sequence, exact hash and authenticated current backend. Capture finite recovery_target_sequence in the FULL transaction closing old signer and transferring original unissued R; enter durable recovering. Advance confirmed only across a contiguous persisted prefix. Multiple bounded batches, including >1000 records, must eventually reach target despite new tail. After restrictions and fresh heartbeat commit, enter online; current post-barrier pending tail remains visible. Receiving a forged/late/duplicate ACK never closes unknown liability; financial settlement requires the original separate proof. Pending/disputed/unknown records are never pruned. Settled records require contiguous authenticated confirmation, >=7 days retention and trustworthy age.

## Share references, CAS and immutable policy transition

Every new 1.2.0 authorization record references its delegation_id and leaf share_reference_id. Full unique parent chain is supplied or already installed durably. Root parent_share_id is empty and parent_revision=0; root must match an authenticated backend source anchor, never an untrusted self-signed object. Each child matches parent ID/revision, source/rule/path and authorized issuer scope; leaf subject_node_id matches receiving node and assigned share. No cycle/missing parent, duplicate ID, chain deeper than32 or overflowing amounts. not_before < expires_at <= hard_deadline and all descendants remain inside parent time/amount limits. Current allowed issuer epoch must not be closed. Node/boot/fencing binding is provided by update/delegation; a new boot retains old references for settlement only until reauthorized. Root source and current expected revisions must be checked against durable local authenticated state, not merely consistency within the message.

Prepare reserves parent occupancy with current revision CAS in the same FULL transaction as configuration holds; aggregate sibling/current pending occupancy cannot exceed parent rate/connections. Commit checks current complete chain, current revision/fence and allowed issuer. Activation-unknown retains both old/new occupancy until execution proof permits release. Duplicate transaction replays exact original result; stale parent revision cannot be repaired by dropping a parent. Engine receives validated immutable scope through formal Agent interface, never SQL or control client access.

Policy transition is a durable transition_id plus complete immutable old/new snapshots, effective_at, optional preissue lead <=24h and guard <=300s (candidate default5s). Both snapshots bind the same rule/node/path; IDs differ and new policy revision increases. Each referenced future source/credit/runtime/share authorization already exists, is valid for the future period/snapshot and conserves its own parent funding. Preissue makes no new balances; signed rights not usable before not_before/effective_at. At a conservative Earliest >= effective_at, new sessions select new snapshot; existing sessions keep their established immutable snapshot within original valid authorization. If Latest crosses the boundary while Earliest has not, deny new ambiguous admission and cap newly issued rights at that boundary. Missing future authority, revocation or untrusted time stops new crossing; cannot fallback to stale policy to gain unfunded service. If cancellation/revision is needed, it must use approved current configuration/revocation semantics with unique transaction and preserved holds, not mutate a settled snapshot. Transition ID replay with changed bytes rejects.

## Identity and deployment defaults

identity-profile.json supplies dual canonical DNS+URI SAN and both EKUs for node certificates. Role-specific Hub/backend identity remains configured and verified separately. VLink expected node DNS is the exact peer UUID plus canonical URI verification; the qualification DNS name "vortex" is not valid production peer identity. Runtime TLS and exporter HELLO capture one immutable cert/private-key signer snapshot; commit before atomic publish. Retained overlap never exceeds original NotAfter or survives revocation. New valid cert serial does not alter node, boot, path generation or financial ownership. Default TLS session resumption remains disabled until freshness tests exist. Revocation/expiry denies new links/OPEN/renewal, including OPEN on already established multiplexed link; accounting liability remains. Existing valid business sessions can drain under original bounds during ordinary rotation.

Runtime validation also requires retry_min_ms <= retry_max_ms, dial_timeout_seconds <= failover_budget_seconds, unique regional Hub IDs/endpoints and unique probe target IDs referencing current local rules.

All timing knobs are configurable development candidates, not a new frozen V1.11 hardware/performance gate. No new libraries/tools are selected. Agent/Hub SQLite migrations are additive, FULL and independently owned; old binaries may run only when compatible reads were demonstrated. Binary rollback never restores authoritative db/WAL, usage, signer closure, fence, unknown holds or credential authority from an old snapshot. Candidate parsing/round-trip tests establish only contract feasibility; actual process lifecycle, disk/race and same-SHA Docker/Actions qualification are subsequent plan gates.

## Authority restriction and handoff inputs

BackendMessage.authority_restrictions is distinct from certificate revocation. Only the authenticated configured backend may supply it; restriction_id/hub_id, monotonic revision and exact payload hash are durable idempotency keys. Persist restrictions before exposing acknowledgment/readiness or processing the associated handoff. Revoked delegation/authorization IDs are additive; no array absence unrevokes one. Each DelegationRestriction is an upper ceiling on total already-issued raw bytes/rate/connections for an existing delegation, may only reduce the previously authenticated cap, never refill funds or increase scope. Zero denies further issuance. If already-issued rights exceed a new cap, stop affected new issuance/OPEN and preserve outstanding funds/unknown debt; do not truncate usage or refund the difference. Limit IDs must be unique even where their complete objects differ. Effective-at uncertainty denies affected new rights. Propagate affected leaf authorization revocations through the existing authorization envelope before offering replacement rights. No financial restriction creates authority to allocate.

BackendMessage.epoch_handoff_request is the typed input to existing Hub Recover/HandoffHub. Require matching authenticated Hub, from_epoch=current open epoch, to_epoch=from_epoch+1 without overflow, current backend heartbeat bound and required_restriction_revision already durably applied. Missing or delayed restriction revision holds the request; never default it to empty revocations. Same handoff ID and identical request replays original S/R result, changed request rejects. Compute S and original unissued R locally in one FULL transaction, close old signer, transfer R once and capture recovery barrier. Backend request contains no arbitrary R amount to mint. Handoff and status revisions are independently persisted; certificate-revocation revision must not substitute for authority restriction revision.

Ledger payload is a replayed immutable observation, not a new allocation request. The phase-5 backend fixture commits the exact Hub/sequence/kind/record ID/bytes/hash before echoing ACK, preserving uniqueness and requiring contiguous confirmation. It does not derive funds, signer authority or financial settlement from arbitrary ledger payload. `issue` payload is the original authorization envelope; `known` is the existing deterministic UsageBatch protobuf and `unknown` is UnknownUsageBatch protobuf; `handoff` is the existing {Request,Result} record from agent/store/handoff.go at the recorded baseline. No payload is reinterpreted as BackendAllocation or EpochHandoffRequest. Any new semantic interpretation/record encoding requires another reviewed schema/version rather than extending an opaque NodeEvent.
# Control 1.5 candidate: provenance-preserving usage ledger

The additive `usage-ledger-v1` capability is negotiated independently of signer
authority. It permits only `LedgerRecord.kind=usage-v1` carrying a UTF-8 JSON
`usage_envelope` from lifecycle.schema.json. Outer Hub mTLS identity must match
the configured Hub and the persisted node-to-Hub assignment. The inner node,
origin/report boot, original NodeMessage sequence and original payload SHA-256
are retained before any acknowledgement. Known usage requires origin boot equal
to report boot. Unknown reports may describe an old origin grant boot while
their tuple belongs to the new report boot. Both bindings must be validated.

Backend LedgerAcknowledgement is sent only after PostgreSQL commits usage,
source ledger, task/outbox and shared audit. Duplicate batch bytes return the
stored acknowledgement; conflicting tuple or digest remains disputed and does
not overwrite history. Sequence-less historic SQLite records remain pending.
This extension does not authorize a signer, allocation, runtime lease or share.
All existing Protobuf field numbers, old messages and legacy wire behavior remain.
CR-007 is pending owner approval; this candidate is not a frozen release claim.


# Control 1.6.0 candidate: funded backend transport

CR-007 has been approved for its exact 1.5.0 artifacts. This appended 1.6.0
candidate is a separate CR-008 proposal under construction; it is not approved.
The old 1.3.0 Hub hello/status paths retain their blocked, signer-epoch-zero
requirements. They cannot transport funds or reopen a signer.

The new HubControl branch requires an exact 1.6.0 hello and ordered capabilities
`[status-v1, backend-allocation-v1, backend-ledger-v1]`, its optional
`[status-v1, backend-allocation-v1, backend-ledger-v1, share-renewal-close-v1]`
extension, or the independent
`[status-v1, usage-ledger-v1]` observation branch. TLS 1.3 role roots are distinct:
backend servers verify the Hub CA and clients verify the backend CA. The Hub ID
is the certificate CN and sole DNS SAN. Hub ID, boot, session, region and increasing
stream sequences bind every exchange. Unknown capability combinations fail closed.

PostgreSQL first binds a preapproved signer epoch to one authenticated Hub boot,
with shared audit/task/Outbox atomicity. A hello cannot create an epoch. A different
boot, a higher epoch without explicit recovery, or an expired approved deadline
cannot reuse old authority. Recovery is not implemented by this branch; retained
money and shares remain held. A same-boot reconnect reuses the fixed approved
autonomy deadline, never `now + duration`. Across current-epoch tenant approvals,
the session uses the earliest approved deadline. Any change closes the live session
so its fixed boundary can be reevaluated conservatively.

A backend heartbeat uses trusted UTC bounds. The Hub waits until its trusted lower
bound proves the sender timestamp is past; it does not widen the window to accept
a future timestamp. Missing trust, excessively wide bounds, stale samples and hard
deadline expiry reject. Heartbeat persistence cannot reopen an autonomous signer.
The initial status is blocked at epoch zero until a finite allocation has actually
committed; later online status must name the approved epoch. Status is observation,
not Node execution proof.

BackendAllocation carries only a committed source-funded parent and three finite
source anchors. Its outer delivery is restricted to the stored Hub boot. The Hub
validates canonical payload bytes and SHA-256, then installs anchors and delegation
in one FULL SQLite transaction. It sends LedgerRecord kind `allocation-installed-v1`
only after commit: record_id is allocation_id, ledger_sequence is the BackendMessage
sequence carrying that allocation, payload is those exact allocation bytes, and
payload_sha256 is their digest. PG commits an immutable installation receipt plus
shared audit/task/Outbox before echoing a matching LedgerAcknowledgement. Replaying
an installed allocation never refills its spent balance. Installation cannot roll
back a fresher authenticated heartbeat or extend its current autonomy deadline.
The receipt says nothing about Node activation and never returns funds.

The Hub uploads actual FULL issuer journal entries using LedgerRecord kind
`issue-v1`; record_id is update_id, ledger_sequence is the original durable Hub
journal sequence, and payload/digest are the exact authorization envelope. The
backend validates the original allocation, signer epoch, Node/boot/path/fence,
share roots and each child's original hard deadline. Envelope liveness can reflect
a newer backend heartbeat; it cannot extend any child's immutable funded boundary.
Child credit IDs partition one original-source parent reservation, without another
hold or per-child rounding. PG journal mapping and audit commit before ACK; only
an exact ACK lets SQLite confirm its contiguous journal prefix. Other journal
kinds retain their existing encoding and remain pending until their specific
recovery/closure consumers exist; they cannot be reinterpreted as new funds.

The independent 1.6.0 usage branch forwards the existing provenance-preserving
`usage-v1` envelope for an active signer. It negotiates no allocation/issue ability.
Node-to-Hub ownership and original Node sequence/boot/hash remain mandatory. Missing
child mapping denies ACK and preserves the original usage for retry. All successful
usage acknowledgements follow the existing PG source-accounting/audit transaction.

This candidate still lacks automatic allocation scheduling, Node anchor delivery,
production runtime NodeControl, explicit boot/epoch recovery and complete parent
closure/remainder return. A delayed first installation outside its original fresh
heartbeat window fails closed; no timeout frees its parent hold. These limits are
not a completed Phase 6 acceptance claim. No protobuf field number changes here.


## Direct Node source provisioning (1.6.0 candidate)

NodeMessage tag 21 is SourceProvisionReceipt; ControlMessage tag 20 is
SourceProvisionCommand. Both contain bounded canonical UTF-8 JSON payload and
SHA-256. All old tags and runtime/authorization semantics remain unchanged.
The Node Heartbeat advertises `source-provision-v1` only on a direct control
client explicitly configured with a canonical source backend UUID (node CLI
`--source-backend-id`). Regional Hub control cannot enable this capability or
send this command. The verified server CN/sole DNS SAN must equal that configured
backend UUID; deployment supplies the dedicated backend CA for this direct client.
The backend sends no source message to legacy or unknown-version capabilities.

The source_provision payload binds version 1, provision_id=allocation_id,
backend_id, node_id, current boot_id, issued_at and exactly three finite roots
from an immutable PG-funded allocation. Every root is parentless, belongs to the
same Node/Hub/epoch/rule/revision/path, and has a distinct source/share ID. Roots
are limits on future authorization validation, not credit grants or runtime
leases. PG validates current Node incarnation and original allocation expiry,
persists exact bytes, operation/task/Outbox and shared audit before transport.

The authenticated Agent validates its local boot and explicit backend identity,
checks trusted conservative time for first installation, and commits all roots,
provision bytes and the deterministic receipt in one FULL SQLite transaction.
Any disk failure rolls the whole bundle back. Same provision ID plus changed bytes
rejects. An exact durable replay can return its original receipt even after time
trust is lost: it creates no new root, balance, lease or listener. A new boot can
never replay the old boot's receipt. The JSON receipt contains version, provision,
backend, Node, boot and original payload SHA-256; its outer protobuf digest covers
those receipt bytes.

Only the currently authenticated Node stream can commit the matching receipt to
PG. Shared audit, immutable receipt, original operation/task completion and Outbox
delivery commit together. They prove root installation only, never runtime
activation or permission to return funds. Transport failure leaves the original
delivery pending and original funding held; reconnect retries exact bytes. The
production backend worker reads committed PG candidates and handles independent
candidate errors without aborting the rest of the current bounded page. Complete
boot recovery, stale candidate maintenance and runtime lease delivery remain
separate work; no timeout drops unknown financial occupation.


## Candidate old-share renewal closure

Only `share-renewal-close-v1` peers receive BackendMessage field 17,
ShareRenewalClosureCommand. The canonical JSON command and SHA-256 bind backend,
tenant, handoff, Hub boot, Node boot, independent share kind, predecessor and
successor generation, signer epoch, one-based part/parts and up to 64 strictly
sorted unique delegation IDs. Every nonfinal page has 64 IDs; a sole empty page
explicitly closes an issuer with no funded delegations. Successor is exactly
predecessor + 1. No rate/connection generation is inferred from a merged value.

In the tenant serialization transaction, PG snapshots every funded old delegation
and commits all pages, tasks, Outbox and audit together. This is an issuance
barrier: new old-generation funding/renewal is denied; exact nonce replays retain
their old bytes. Queued late installations covered by a barrier are not sent.
The Hub validates backend/Hub/boot and each installed delegation's Node/boot/epoch.
One FULL transaction closes installed issuers, writes tombstones for uninstalled
IDs, and persists immutable command/receipt. Later installation cannot resurrect
them. Closure requires no trusted clock, creates no authority, and preserves all
already issued child leases and original financial holds.

LedgerRecord kind `share-renewal-closed-v1` carries canonical receipt JSON and its
SHA-256. record_id is command_id; ledger_sequence is the BackendMessage sequence
of that command. The receipt binds command_id and command_sha256. PG authenticates
the Hub role and boot and matches the pending command before committing the page,
its audit and delivery completion. Only all pages together close the predecessor
renewal authority; this does not release capacity. Only a subsequent real Node CAS
receipt may release the proven difference. Lost FULL receipt causes exact command
replay; a lost PG ACK cannot duplicate closure. Older funded and usage-only peers
cannot receive or acknowledge closure. These are candidate CR-008 artifacts.


## Candidate funded delegation seal

The independently negotiated `delegation-seal-v1` extends the ordered funded
capabilities after `share-renewal-close-v1`. Three- and four-capability peers
remain accepted, but may not send `delegation-sealed-v1`. The production Hub
advertises all five capabilities. An expired or renewal-closed delegation can
be sealed in one FULL transaction: permanently stop new issuance, retain prior
request replay bytes, persist an immutable seal and append its issuer journal.
A trusted lower time bound must prove expiration; a closed issuer needs no
assumption about Node stop. The seal is not child-stop evidence.

The complete inventory contains issue ID, SHA-256 of exact issue payload and raw
amount, strictly sorted by issue ID. `issues_sha256` hashes its canonical JSON
array (empty is `[]`); issue_count and issued_raw_bytes bind its cardinality and
sum. raw_bytes = issued_raw_bytes + unissued_raw_bytes. LedgerRecord kind
`delegation-sealed-v1` uses seal_id and the durable Hub journal sequence, payload
is canonical seal JSON, and payload_sha256 binds it. The normal journal uploader
sends earlier issue records before seal. PG independently reconstructs the full
inventory from previously accepted authenticated issue records before accepting
any return. Missing or different inventory stays pending, without release.

The PG seal, source-local return, audit/task/Outbox commit together before ACK.
Only unissued_raw_bytes can return. All issued child bytes, including expired or
unknown children, retain their original source hold with the single parent
coefficient and rounding. No-child complete inventory can finalize zero actual
usage; this cannot be inferred from expiry alone. Exact lost ACK replay returns
nothing twice. After sealing, only previously recorded issue IDs may replay;
new child mapping is rejected. Child final accounting, safe sustained automatic
refill and explicit boot recovery remain separate work; CR-008 remains candidate.

## Candidate allocation aggregate dependencies

Funding persists immutable `hub_allocation_share_bindings` for both Node aggregate
kinds under the same tenant lock and audit/task/Outbox transaction as its parent
reservation and allocation. Rate and connections retain independent generations;
source-reference generation starts at1 for each fresh source UUID and cannot
stand for either Node aggregate generation. Closure snapshots join the exact
kind, predecessor generation, signer epoch and Hub/Node boots, including issued
and not-yet-installed allocations. A missing or inconsistent dependency stays
unknown and cannot become an empty closure proof. Existing source-reference wire
semantics and historical approvals are unchanged; these DB artifacts are candidate
CR-008, not an approval or an inferred Node-stop receipt.

Sealed allocations are never candidates for backend redelivery, including when
their earlier installation receipt was lost. A persisted installation receipt,
issue mapping or seal is evidence that this Hub boot has already held issuer
authority: a reconnect reporting empty/blocked epoch0 must not bootstrap it again.
Its existing FULL state may reconnect and replay exact journal records; missing
state requires the separate explicit recovery workflow. This check neither
releases unknown debt nor treats an unseen receipt as non-execution.

The generic exact-reconciliation receipt is also insufficient for a funded parent:
it cannot finalize or recover that parent while bypassing the complete issued
child/Node closure proof. This applies both to unresolved and risk-budget-settled
parents. Ordinary nondelegated exact/risk reconciliation keeps its own existing
semantics; issued delegated funds remain frozen pending dedicated closure.

## Direct Node credit closure (1.6.0 candidate)

Independent `credit-close-v1` is advertised only by a direct Node client with
an explicit canonical backend UUID pinned to the verified TLS role identity.
Regional Hub and legacy unpinned streams cannot execute these commands. Existing
tags/behaviors remain unchanged; `NodeMessage.credit_close_receipt=22` and
`ControlMessage.credit_close_command=21` carry canonical JSON plus its SHA256.
`contracts/credit_close.go` defines strict bounded command/result envelopes.

The command binds its immutable UUID, backend/Hub/Node/boot/grant identities,
path generation and original funded1.2 Hub issue bytes. The Node validates the
original against its current boot and Hub reference issuers without applying
it, refreshing time or creating leases/roots. Installed grants require matching
original lineage/amount/times (excluding the locally observed ShareRevision).
Uninstalled grants require a FULL permanent identity tombstone before any
zero-use witness; delayed matching installs remain revoked and changed scope
is rejected. A new boot cannot manufacture absence evidence for a prior boot.

Stop permanently denies further pre-write tickets. It never resolves existing
tickets or closes sockets by assertion. Pending/unknown outcomes produce only
`unsettled` without final evidence; expiry/ACK cannot replace a known outcome.
Known terminal tickets, original payload digests and source conservation are
verified in one transaction before an immutable local final snapshot is returned.
The final JSON is the candidate `agent/store.CreditGrantFinal` record, including
the complete `AuthorizationRecord`, Used/Unused, BatchCount/UsageSHA256, explicit
version1 `KnownUsage` inventory and optional NeverInstalled witness. KnownUsage
binds sorted committed batch IDs and original payload SHA256 values; returned
local tickets remain solely in the local forensic digest. Historical version0
finals are incomplete and must not authorize financial release. The outer result binds the command SHA256 and exact
target identities. Lost transport receipts replay durable evidence without
reopening credit. This snapshot is evidence for a future dedicated PostgreSQL
consumer, never financial acceptance or release authority by itself.

Node and backend actual TLS1.3 tests cover capability isolation, pending tickets,
lost FULL receipts, reconnect replay and PG known-usage arrival. Backend durable
close commands derive only from accepted complete Hub seals; authenticated child
receipts pin the original issue and independently reconstruct the known wire
inventory. Unknown liabilities and disputes remain held. Child acceptance alone
does not release parent funds. A separate PG transaction revalidates the complete
sealed issue inventory, every authenticated child final and its known wire
inventory, then settles the single original parent with its original coefficient
and source remainder. Missing children, unknown liabilities or disputes deny
release. Automatic funding successors require all matching predecessors to have dedicated complete parent finals (or true zero-child seal finals). Stable finalized allocation/reservation inventories bind nonces and are rechecked under the tenant lock; expiry, exhaustion and conservative estimates never authorize refill. Explicit boot recovery remains pending. CR-008 is unprepared/unapproved and
earlier approved contracts are unchanged.

## Runtime/share lease refresh and independent heartbeat (1.6.0 candidate)

The funded HubControl branch independently opts in with the exact ordered
six-capability list `[status-v1, backend-allocation-v1, backend-ledger-v1,
share-renewal-close-v1, delegation-seal-v1, runtime-lease-refresh-v1]`.
The prior three/four/five-capability and usage-only branches cannot submit the
new `runtime-lease-v1` journal kind. Its record ID is the original update UUID;
sequence, exact bytes and SHA-256 identify one immutable FULL journal entry.
PG stores a separate immutable receipt, task/Outbox and audit in one transaction
before ACK. These records never enter the issue/credit inventory or reserve,
charge, return, finalize or otherwise alter financial authority.

Regional NodeControl independently appends `runtime-lease-refresh-v1` to its
existing negotiated capabilities; this does not imply `renewal-v1`. Once an
initial funded credit issue exists, an open current signer may refresh exactly
the runtime/share pair when the conservative lower clock bound proves60s since
the last issue/lease. Each lease has at most300s TTL, clamped to its immutable
allocation, source roots, policy and autonomy boundaries. Original Node/boot,
epoch, path/fence, delegation, sources and rate/connection limits stay bound.
No credit, handoff, revocation or policy transition may accompany this pair.
Existing-session lease replacement retains its original credit; exhausted or
expired credit remains unusable regardless of the new leases. Scanning is by
authenticated Node incarnation across delegations, rather than its last rule.

A separate10s FULL envelope contains no authorizations or policy changes.
It carries authenticated backend heartbeat evidence through the existing Node
authorization persistence path. HubStatus remains observation only. Heartbeats
do not postpone60s lease refresh, create rights or extend credit deadlines.
The existing30s heartbeat recovery guard stays intact: a gap that crosses that
boundary still requires explicit recovery. Revision-based exact replay/reopen
retains original bytes and time; missing clock trust, closed/sealed delegation,
changed roots or hard expiry fail closed. PG independently enforces both
cadences, including the first heartbeat relative to the initial credit issue.

Production tests include a real60s interval, actual Agent FULL installation,
separate backend/Hub/Node TLS1.3 role roots and lost FULL receipt/PG ACK replay.
Sustained credit closure/refill across expiry and explicit boot/epoch recovery
remain separate acceptance work. This is still an unapproved CR-008 candidate;
no previously approved contract snapshot is modified.

The production seal scanner also starts the existing closure protocol when a
delegation is fully issued and the trusted lower time bound proves that every
original child credit has expired, even if the parent delegation lasts longer.
It verifies immutable issue payload hashes, original Node/boot/source/amounts
and exact total conservation; unissued balance or any unexpired child prevents
this early seal. Expiry still proves no child stop or known outcome. Backend
child final receipts, independent known-usage inventories and original-parent
settlement remain mandatory before a funded successor can be reserved.
Actual role-mTLS tests exercise this cycle with a short original child TTL,
both local aggregate caps, direct source provisioning and Agent readiness for
the new parent. Existing sessions cannot move arbitrarily to the fresh source.

The candidate backend also exposes an explicit platform-only cold Hub recovery
service. It binds one preapproved newer epoch to one specified fresh Hub boot
only after the trusted UTC lower bound proves every prior tenant authority,
immutable delegation autonomy boundary and immutable share hard boundary has
expired. Every previously authorized tenant needs a current successor epoch
approval. The complete ordered original inventory and time bounds are retained
in an immutable recovery record with shared audit/task/Outbox commit. Concurrent
targets cannot split ownership; an audit error rolls back the new incarnation.
This path transfers no unissued balance, releases no financial or capacity hold
and supplies no Node execution-stop or final-usage proof. Late original usage
still charges its original source. Old boots remain denied. New empty Hub state
can connect but remains without runtime rights until independently prepared,
funded successor authority arrives. Local journal sequence identity includes
Hub boot, preserving old evidence when fresh SQLite starts numbering again.
The HTTP entry, early S/R recovery, same-store epoch recovery and Node boot
recovery remain separate implementation work. This is an unapproved CR-008
candidate, without changes to previously approved contract snapshots.

The production renewal-closure worker may consume that immutable cold Hub
proof for the exact original predecessor share recorded in its inventory.
It checks the proof hash, bound successor Hub incarnation and the current
trusted lower time bound before closing issuance. Already queued live closure
pages remain immutable: unacknowledged work is retired as failed with its
Outbox consumed locally, and no Hub receipt is fabricated. Previously accepted
page receipts retain their outcome. This retirement and the closure proof share
the tenant audit transaction. A pending live closure cannot hide later eligible
cold-proof work from the worker. Current successor shares are excluded from the
retired-epoch inventory; their hard boundaries stay independent.

Closure still does not release a capacity difference: the original Node boot
must durably CAS the next linear rate/connection generation and return its
matching receipt. Original child credit and parent financial holds remain.
Tests also independently prepare/fund a fresh rule at the approved new epoch,
FULL-issue from fresh Hub SQLite with local ledger sequence1, and accept both
new-boot sequence1 and the exact old-boot sequence1 replay. This does not claim
that a prior prepared/unknown rule has converged or that Node boot recovery is
complete; those original configuration/port holds remain protected.

A separate candidate local Node cold-boot consumer retires both original Node
aggregate heads under SQLite FULL. The canonical backend command pins tenant,
Node, backend, old/new boot, exact original rate and connection commands, their
independent generation floors, a strictly greater fencing floor, trusted
safe-after/issue/expiry bounds and the immutable authority-proof digest. Any
later old-boot hard deadline already stored on the Node independently prevents
early retirement. No credit, old execution receipt, activation release or
financial final is created. Unknown original evidence and balances remain.

Fresh disk installs only zero-capacity retired heads at the original per-kind
floors. Retained disk must match both exact original heads; a higher/different
head denies the whole transaction. Neither head may activate until a subsequent
new-boot linear CAS advances that kind. Same-command FULL replay after restart,
expiry or later CAS returns its original canonical receipt and never lowers a
head. Conflicting commands and corrupt stored receipts fail closed. Concurrent
new boots cannot split the two kinds across owners. This is a local primitive
only: it is not yet connected to NodeControl, and complete PostgreSQL proof,
explicit approval, independent capability negotiation and authenticated receipt
consumption are required before production use. It does not complete Node boot
recovery or authorize manual blocked-to-active state changes.

The candidate PostgreSQL Node cold-recovery producer now requires an explicit
platform Grant decision for one already authenticated new boot. Under an
exclusive transaction-level Node authority lock it records all tenants' original old-boot aggregate
generations, funded allocation payload digests and original autonomy bounds,
lease expectations, and configuration member/deadline inventory. The trusted
lower time bound must pass every included boundary. New share, lease and funded
allocation producers take the corresponding shared lock before their current-incarnation check;
a concurrent old transaction cannot disappear behind the snapshot. A late
original issue remains bounded by its already inventoried immutable roots.
Configuration deadlines do not attest Engine stop or release configuration
holds. A later hard deadline known only to Node SQLite still denies retirement.

This first producer supports a pair of fully acknowledged aggregate heads;
missing, pending or unknown CAS remains held. The original two complete commands
are checked against every immutable generation field. Approval, canonical
command, task, Outbox and audit commit together. Retries return the original
bytes, including the original expiry; no retry silently creates fresh authority.

The authenticated canonical FULL receipt consumer migrates only that tenant's
aggregate boot ownership and records each independent `retired_generation`.
Active generation becomes zero; head generations and held capacity stay intact.
Old-boot renewal/fencing close under the same audit transaction. A new-boot share
must advance each retired kind by exactly one and pass current eligibility and
all existing CAS checks. Only its real subsequent Node CAS receipt clears the
retired marker and can release the exact capacity difference. Every financial,
old execution and configuration fact remains independent. Receipt replay after
expiry or a later CAS never rewinds the chain. This backend service prerequisite
still needs independently negotiated pinned direct-backend NodeControl delivery
and its production worker; it is not yet complete production Node boot recovery.
The candidate remains unapproved under CR-008.

Candidate NodeControl delivery now has independent `node-boot-recovery-v1`
negotiation. Additive NodeMessage23 and ControlMessage22 carry canonical
payload/SHA envelopes; all existing tags remain unchanged. Only a direct Agent
client configured with an explicit canonical backend UUID advertises this
capability. Regional Hub streams cannot send it; source/share/credit-close or
unknown future capability names do not imply recovery authority. The TLS server
identity must match the configured backend UUID before stream startup, and the
command independently pins that identity and the Node's actual current boot.

The production worker delivers only existing immutable platform-approved
commands. No worker automatically approves a recovery, changes its expiry or
infers success from sending bytes. The stream handler checks Node identity,
boot, sequence, pending command and receipt digest before the PostgreSQL service
validates the complete canonical FULL receipt and commits it with audit/tasks/
Outbox. Lost Node FULL receipts reconnect and replay the original bytes while
PG keeps the original boot and capacity occupation until acceptance.

A first local retirement uses bounded trusted time; an untrusted boot clock may
observe the command under the existing continuity rules and bootstrap from the independently authenticated command's issued time within the
existing300s boot freshness rule. The command creates no Hub heartbeat. Exact
already-FULL replay is checked before clock bootstrap, so command/clock expiry
cannot force a different command or rewind later generations. A stale first
installation still fails. These additive candidate tags and semantics remain
unapproved CR-008 work, separate from original-rule execution convergence,
same-store signer S/R transfer, HTTP entry and complete Phase6 acceptance.

### Candidate funded same-store S/R primitive (not yet a production protocol)

The local Hub HandoffRequest may carry BackendID and FundedSuccessors. Every
open funded delegation not explicitly revoked must have exactly one successor
binding containing the SHA256 of its original SQLite HubDelegation payload and
exactly three flat source roots. This local payload digest is distinct from the
original backend Allocation wire digest; a PostgreSQL consumer must independently
link them. A root advances signer_epoch and revision by exactly one. Source,
share generation, rule/path, rate/connection limits and all time bounds stay
identical. The BackendID must own the existing source anchors. Original
delegation payloads, balances, issued partitions and unknown debt remain intact.
The bindings, roots, revocations, epoch closure, R transfer and handoff journal
commit together under SQLite FULL. Missing/extra bindings, stale roots or any
write failure roll back the entire transition. A historical generic handoff
cannot silently move a funded delegation without those explicit bindings.

FundedInventory commits the complete retained journal prefix through S. Its
PrefixSHA256 hashes newline-delimited canonical JSON objects with fields
Sequence, Kind, ID, SHA256 in that order, ascending from sequence1 through S.
Each digest is checked against retained payload bytes, including already ACKed
records. Every transferred delegation also commits its original payload digest,
total raw bytes, issued total, R, issue count and the existing sorted PartitionDigest.
All original issue rows must be present in that journal; issued+R must equal the
original raw amount. Handoff/transfer witnesses and handoff journal bytes are
immutable; only journal confirmation may advance.

Effective funded authority is derived from the retained attested chain instead
of rewriting original allocation bytes. New credit and runtime/share refresh
wait for CompleteRecovery's finite contiguous ACK barrier, including the handoff
record. Closing issuance and retaining debt need no fabricated ACK. Historical
issues replay their original bytes. Subsequent seals include child partitions
from every earlier signer epoch under the original delegation, with no new
parent reservation or source conversion.

Funded successor envelopes retain authorization1.2, all share references and
policy restrictions. Epoch handoffs include the credit source's R and two
nonfinancial runtime/share source proofs with zero bytes. Node FULL persistence
requires each source's original local family; backend anchors for the new roots
must arrive independently. Existing grants may drain under their immutable old
roots, while current admission uses the new roots. A fresh/lost Hub store is not
evidence of same-store S/R; cold recovery remains a separate boundary.

This increment has local FULL/fault evidence and actual Hub/Node TLS transport
with explicit provisioning/ACK fixtures. It does not add backend approval,
complete PG prefix/partition verification, recovery preflight capability,
automatic direct-Node reprovisioning or a production S/R consumer. Those remain
required before enabling the path. No new wire tag/capability is implicitly
negotiated here and CR-008 remains unapproved.


### Candidate PG same-store recovery approval (preflight still required)

PrepareHubSameStoreRecovery requires an explicit platform Grant decision, the
exact currently bound Hub boot/epoch, a preapproved adjacent next epoch, fixed
target boot and the backend that owns every existing source provision. All
previously authorized tenants must approve the next epoch. Every original,
unsealed Hub allocation must have its authenticated installation receipt and
persisted Node source provision. A sealed allocation is not transferred. Older
unsealed boots/epochs cannot be silently adopted into R. This first PG approval
producer supports allocations from the immediately preceding original epoch;
repeated PG transitions require a separately verified effective-authority chain.

The immutable command links both original backend Allocation wire SHA256 and
original local HubDelegation payload SHA256. Its three successor roots change
only signer epoch and root revision, each by one. Source, share generation,
limits, Node boot, rule/path, policy and all original deadlines stay fixed. No
arbitrary R amount is accepted from an operator. Exact replay returns the same
command/proof and operation even after clock expiry; it does not retime authority.

The command, all-tenant approval inventory, original allocation links, task,
Outbox and audit event commit together in PostgreSQL. A global Hub incarnation
lock serializes competing targets; tenant financial state is not changed.
Preparation does not bind the target incarnation, acknowledge a ledger record,
close old renewal, complete an operation, release capacity/money or make a Hub
online. The local FULL command can produce the complete S/R manifest while
remaining behind its finite ACK barrier. Independent capability negotiation,
authenticated boot-bound preflight, complete PG journal/partition verification,
new-root Node provisioning and effective issue/lease/seal consumers are still
required. No recovery wire handler or approved snapshot is added by this unit;
CR-008 remains unapproved.


### Candidate retained prefix and independent PG partition witness

LedgerPrefix reads every retained local row through a fixed boundary, including
already confirmed rows, in continuous sequence order. It rejects missing rows
and payload hash mismatch. Pages have count and byte bounds; reading never
changes confirmations or usage settlement.

RetainHubSameStoreRecord accepts only the authenticated approved target Hub boot
and exact original record identity/digest. Each immutable PG journal row,
pending obligation task, Outbox and audit commit together. Out-of-order retention
is permitted; it proves no contiguous completion. Replaying retained bytes does
not retime a command. A verified manifest forbids adding new prefix records.

VerifyHubSameStorePrefix independently hashes every sequence1..S and requires
the exact local handoff record atS+1. It checks the original approved command,
complete prefix digest and each transferred delegation's complete independent
PG financial issue inventory. Each prefix issue must already have its canonical
PG financial receipt with the original allocation, boot, sequence and bytes;
every PG issue from the old boot must occur in the prefix. A shorter but
self-consistent forged prefix cannot omit an independently registered child.
The original raw amount, issued count/total/digest and R must all agree. A
transferred allocation sealed meanwhile is refused. All prior tenant locks are
acquired in sorted order under the Hub incarnation lock, preventing issue
registration from changing the inventory during verification. Changed authority
inventory, replaced boot, advanced epoch, command expiry and uncertain time deny
first verification. The verified witness/task/Outbox/audit are atomic.

Retention and this immutable witness are neither a local journal ACK nor a Node
usage settlement. Known/unknown usage remains a pending obligation; it is not
silently charged, finalized or removed. Financial debt/capacity and original Hub
binding stay unchanged. Exact witness replay remains available after clock
expiry. It cannot establish fresh readiness: admission must revalidate current
authority, complete partitions and original seals/closures, independently
provision Node roots and consume the original FULL handoff. No recovery transport,
new capability, online binding or approved snapshot is introduced by this unit.


### Candidate Node credit-inventory fence for early same-store recovery

PG prefix reconstruction cannot discover a child installed at Node but not yet
reported to PG if a Hub snapshot is rolled back. Early admission therefore also
requires an independent direct-backend Node credit-inventory fence. This local
primitive is not yet exposed by NodeControl and has no negotiated wire handler.

RecoveryCreditFence fixes backend, recovery/manifest digest and sealed sequence,
Hub, actual Node boot, original parent/source/billing snapshot, original credit
root, adjacent signer epochs, original raw budget, complete expected grant IDs
and amounts, issued total and R. The bounded canonical command supports at most
4096 expected credits and one MiB; larger inventories remain unsupported/held.
The existing original source anchor must match its exact backend-owned bytes
and SHA. Current source epoch must equal from_epoch. Every locally installed
original-source credit, including revoked/closed historical records, must match
an expected ID/amount and original parent/issuer/boot/snapshot/rule/path. Unknown
or unreported local children deny; a cold/missing original Node incarnation
cannot assert old scope. An expected but never-installed child is allowed, but
its issued budget cannot become fresh R.

Command, immutable installed-subset receipt and a source-specific admission
fence persist atomically under SQLite FULL. It installs no root/authorization,
changes no global generation/clock or Engine state, and releases no existing
unknown liability. Once committed, new old-epoch credit is refused; already
installed rights remain under their original bounds. The subsequent credit
handoff must match the exact recovery ID, original parent, adjacent epochs, R,
sealed sequence and manifest digest. All new child credit is independently
bounded both by the original raw total and the approved new-epoch R, including
when a mutable handoff remainder is corrupted. Future epochs require another
explicit adjacent fence; no implicit floor advance is allowed. Exact receipt
replay after reopen or clock expiry returns original bytes without reapplying
or lowering later state.

This increment supplies the Node local primitive and rollback/race tests only.
Backend immutable producer/receipt verification, independently negotiated direct
transport, next-root provisioning and production S/R admission remain required.
A local fixture success does not prove a PG-approved current Node receipt or
physical same-store continuity. No wire capability/tag or approved snapshot is
changed here; CR-008 remains unapproved.


The initial Agent/root race and combined Linux component race also exercise the
current boot failover tests. Under simultaneous long native PG race load, the
bounded six-second autonomous regional failover fixture timed out; the same
Linux autonomous subtest passed isolated in1.979s and a serial complete Linux
Agent/Store/Hub/contracts/Node component retry passed. This is a resource-load
fixture timeout, not a relaxation of the failover bound.


## Direct Node original-credit recovery fence (candidate)

The direct Node Heartbeat independently advertises `node-credit-recovery-v1`
only when an explicit source backend UUID is configured. It is not a regional
LifecycleHello/HubStatus capability. Legacy source/credit-close/boot-recovery
capabilities and unknown future versions do not permit this command. Candidate
NodeMessage tag24 and ControlMessage tag23 carry exact payload bytes and SHA-256.
No approved snapshot is changed; this remains part of the unapproved CR-008
candidate until the complete review package is ready.

After a complete immutable PG same-store manifest has been independently
verified, a platform Grant may prepare one immutable fence per original
allocation/recovery. The transaction locks Hub incarnation, Node authority and
tenant; rechecks current old Hub epoch/boot, current Node boot, original unsealed
allocation digest, backend-owned provision roots and PG child totals; and pins
manifest SHA, sealed sequence, original raw/issued/R and sorted child IDs/amounts.
The original parent is the delegation ID, not the separate PG reservation ID.
The first-install expiry is bounded by the recovery command, original credit
root and hard deadline. Exact replay never retimes it. Operation/task/Outbox and
audit commit together before the production delivery worker can send it.

Only the UUID-pinned dedicated-backend mTLS channel can install this fence at
Node. FULL exact replay precedes clock bootstrap; a first install still needs
trusted bounded time and the existing boot clock continuity rules. Regional
Hub commands are rejected. The original local FULL inventory checks and delayed
old-credit fence apply atomically before the deterministic receipt is emitted.

PG accepts only canonical authenticated current-boot receipts for the exact
stored command/digest. Installed children must be a sorted exact-amount subset
of the expected PG list: a Hub issue may not yet have reached Node, and its
absence cannot increase R. Receipt, command operation/task/Outbox completion and
audit commit together. Lost FULL receipt reconnects replay the same bytes; an
unacknowledged command remains pending. No financial/usage/share authority row,
Hub incarnation, prefix ACK or online readiness changes here. Complete current
state admission, independent successor root delivery and effective new-epoch
issue/lease/seal consumers remain required before production S/R can resume.


## Independent Hub S/R preflight stream (candidate)

Production Hub performs a separate `hub-sr-preflight-v1` hello before attempting
the ordinary funded stream. The hello uses1.6.0 with exactly this one capability,
actual Hub boot, region and a fresh session UUID. It cannot be combined with
ordinary funded, lease or usage capabilities. Legacy clients retain their
existing paths. A normal available boot receives an advisory
`funded_session_available`; the client then opens a fresh stream and still runs
all ordinary BindHubSession/funded checks. This advisory creates no authority.
An unapproved replacement boot fails closed; the CLI prints its actual boot for
a subsequent explicit platform approval, without accepting a supplied boot ID.

A matching explicit immutable PG recovery approval is returned in typed
BackendMessage.recovery_command(tag18), binding backend/Hub/actual boot/session/
region and exact command SHA. The Hub validates both outer and payload bindings,
uses existing original-root funded FULL S/R, and replays the exact local result
before checking a clock. First installation needs trusted bounded time within
the command window and all existing HandoffHub boundary checks. No command can
invent R, extend original allocation bounds or substitute fresh roots.

HubMessage.recovery_record(tag13) sends exact retained local rows in bounded
32-row pages through S+1, including already-confirmed original rows. Each PG row
commits via the independent immutable forensic-retention audit transaction.
HubMessage.recovery_manifest(tag14) then requests full PG prefix/partition
verification. Typed BackendMessage.recovery_retention(tag19) pins every scope,
record identity/kind/sequence and SHA; `manifest` is a separate final retention
kind. The client checks each receipt before advancing its in-memory upload
cursor. Lost row or manifest receipts reconnect from the original finite prefix,
reusing the same immutable FULL result and PG rows.

These typed retention receipts never invoke ConfirmLedger, CompleteRecovery,
RecordBackendHeartbeat, BindHubSession or financial settlement. Even after the
complete PG witness is retained, the local confirmed prefix remains unchanged,
the Hub stays recovering and original PG boot stays bound. The client reports
`ErrRecoveryEvidenceRetained` and waits for independent admission. This candidate
delivers the prerequisite evidence; production admission, Node successor roots
and effective new-epoch issue/lease/seal consumers are still required. Prior
approved snapshots remain unchanged and CR-008 is not approved.

## Independent Node successor source roots (candidate)

An explicit platform Grant can derive one immutable successor source provision
per approved S/R allocation only after PG has the complete verified manifest and
the current Node's authenticated FULL inventory-fence receipt. Hub incarnation,
Node authority and tenant locks stabilize preparation; changed authority,
intervening seal, changed financial child inventory, replaced Node boot or
expired original/recovery boundaries deny a first preparation. Installed children
missing at Node stay occupied in the original issued partition and cannot become R.

The provision has a new deterministic ID. Its three flat backend-owned roots are
exactly the original roots with adjacent revision and signer epoch: original
source, Node, share generation, limits, rule/path and every deadline stay fixed.
Original allocation and original source-provision bytes remain immutable. Command,
operation/task/Outbox and audit commit before transport. Separate immutable PG
source-provision/receipt tables link the recovery, allocation and FULL fence.

The production delivery worker uses the existing direct-only UUID-pinned
`source-provision-v1` channel. Hub/regional issuance cannot supply these roots.
The adapter chooses a distinct recovery receipt consumer from its pending request;
an ordinary source receipt cannot satisfy a recovery command. Node atomically
installs all roots under FULL. Exact persisted replay precedes clock observation;
a first install may intersect authenticated time with existing conservative bounds,
without bypassing boot-clock continuity/bootstrap or original expiry checks.

Canonical authenticated current-boot receipts mark only this installation task
complete. Lost FULL receipt reconnects replay identical bytes; failure remains
pending. Roots carry no executable credit, grant no new share capacity, ACK no
Hub journal and do not bind/admit a successor Hub. Current-state all-tenant
admission, effective new-epoch issue/lease/seal and RuntimeServer recovery updates
are still required. This remains an unapproved CR-008 candidate and changes no
prior approved protocol snapshot or protobuf tag.

## PostgreSQL current-state S/R admission (candidate)

Forensic retention and source-root receipts alone never bind a new Hub boot.
An explicit platform Grant calls the separate current-state admission transaction.
It takes the global Hub incarnation lock, sorted Node authority locks, then sorted
all-tenant locks. It reruns the complete original prefix and financial partition
verifier, rather than replaying an old manifest as current readiness. All current
unsealed allocations must be exactly the approved allocation set; new old-epoch
issues, missing children, changed approvals or an intervening seal deny admission.

Every allocation requires the current Node boot/assignment/fencing floor, current
tenant/rule/package/grant eligibility and independent original rate/connections
share bindings to remain enforced without pending replacement or renewal closure.
Original allocation/share/source deadlines remain finite. Every authenticated FULL
inventory fence and successor-root receipt must match the same recovery, manifest,
Node, original allocation, source budgets and exact approved next roots. An active
or pending share omitted from the transfer cannot acquire a new epoch implicitly.

The immutable admission proof includes all tenant approvals, original shares,
manifest/approval hashes and exact Node fence/root receipt hashes. In the same PG
transaction it closes old renewal issuance, binds the one approved target boot to
the adjacent epoch, writes the admission operation/task/Outbox and audit. Audit
failure rolls back closure, incarnation and proof together. Exact same-owner replay
returns the historical bytes without retiming or reopening a later epoch.

Admission changes no original allocation/source/share generation, capacity held,
financial reservation, known/unknown usage or settlement. It does not send a
LedgerAcknowledgement or invoke local CompleteRecovery: the Hub remains recovering
and refuses fresh issue until a separate authenticated admission/ACK protocol is
implemented. This is a platform service primitive, not yet a production preflight
admission consumer or HTTP operation. Effective new-epoch issue/lease/seal/renewal
consumers and repeated PG recovery chains are still required. CR-008 remains
unapproved and approved snapshots remain immutable.

## Effective allocation consumers after committed S/R admission (candidate)

New issue, runtime/share lease refresh, empty runtime heartbeat and delegation
seal receipts use an effective projection of the immutable original allocation.
The projection follows every adjacent PG recovery command, verified manifest,
committed admission and exact authenticated Node FULL fence/root receipts. Each
step must link the original wire allocation and delegation hashes, original
sources and financial parent; only root revision and signer epoch advance.
Every original generation, amount, limit, validity and hard deadline stays fixed.
A retained manifest, an epoch column or an approval without admission is insufficient.

Under the global Hub lock before the tenant lock, new records require the current
bound boot and epoch. Issues and runtime envelopes carry exactly the latest three
handoff proofs: original R for the credit source and zero for runtime/share sources,
with the approved transfer, sealed sequence and exact manifest hash. Earlier PG
admissions remain part of the derivation; the payload need not repeat the whole
historical chain. Issue time cannot predate the admitted recovery's trusted lower
bound. The original aggregate raw cap applies across every epoch. No new financial
reservation, per-child rounding, original root rewrite or share capacity is created.

A new-epoch seal must cover the complete original and successor issue inventory.
Only never-issued raw can return; all issued and unknown debt stays held until
independent authenticated final evidence. Fresh runtime records after sealing deny.
Exact historical receipts replay before current-epoch/time gates, retain their
original boot/sequence/bytes and cannot be relabeled under the successor boot.
All receipt mutations keep audit, task and Outbox atomicity.

These consumers are exercised over ordinary funded role-mTLS after a test fixture
explicitly completes the finite local ACK barrier. They do not implement production
admission/ACK transport, new-epoch generation renewal or repeated PG recovery
producers; those remain pending. No approved snapshot or protobuf tag changes here.
CR-008 remains unapproved; Phase6 remains8/11 and06-06 incomplete.

## Independent authenticated admission and finite FULL prefix ACK (candidate)

The ordinary preflight capability remains forensic-only. A separate exact
`hub-sr-admission-v1` hello and typed `BackendMessage.recovery_admission` at tag20
can deliver an already committed platform admission. The envelope pins backend,
Hub, actual boot, session and region; the canonical payload pins recovery,
adjacent epochs, PG admission hash, complete manifest/prefix hashes, sealed
sequence and a fresh bounded observed time. The backend does not create a
platform approval from a Hub request. It checks the current bound epoch/boot,
complete original all-tenant approvals, original deadlines, current Node
boot/assignment/fence, eligibility and active share bindings without replacement.
Delivery audit is transactional and failed audit emits no capability.

The Hub replays a prior FULL admission before clock observation; replay returns
the original payload without retiming liveness. First installation requires
trusted local conservative time within5seconds of the authenticated timestamp,
recovering mode, the exact active handoff and open target epoch, and every original
autonomy deadline still unexpired. This explicit immutable-admission path may
refresh recovering liveness after the ordinary30second heartbeat gap. It cannot
revive offline, expired, closed or a different recovery, and ordinary heartbeat
checks remain unchanged. The original hard deadline is clamped, never extended.

One SQLite FULL transaction revalidates the immutable handoff and every local row
including confirmed rows; it reconstructs the complete prefix hash and verifies
the exact sealed_sequence+1 handoff row. It confirms only that finite prefix,
updates its checkpoint, stores an immutable admission receipt, fresh heartbeat
and recovering->online transition, and marks recovery complete atomically. A tail
beyond the barrier is not implicitly confirmed. Local or PG admission never settles
usage or releases issued/unknown debt, changes R or rewrites allocation/source/share.

Lost response before local FULL leaves the prefix unconfirmed; SIGKILL before/after,
FULL failure, concurrent connections and reopen preserve atomicity and exact replay.
After local FULL, a lost funded status reconnects through separately gated ordinary
funded authority. The production Hub enables independent preflight plus admission;
legacy callers remain opt-in. The server treats an admitted boot as issuer evidence,
so an empty DB cannot use blocked/fresh funded bootstrap to adopt that epoch.

Platform orchestration of approval/fences/roots/admission is still separate; the
transport consumes only explicitly persisted approval and admission records. Share
generation renewal/closure across epochs and repeated PG recovery producers remain
pending. CR-008 is unapproved, and prior approved snapshots/tags remain unchanged.

## Effective generation renewal and closure after admission (candidate)

An original generation and its original closed renewal authority remain immutable
across S/R. Backend renewal derives an effective signer from adjacent immutable
admissions containing that exact generation, all original allocation bindings,
complete manifest and authenticated Node fence/root witnesses. Current Node boot,
fencing, eligibility, signer approval and original deadlines still apply. A new
nonce at an old epoch denies; exact historical nonce replay returns original bytes.
No original row is reopened and no capacity or money is added.

Before a generation successor can be delivered, the backend freezes a sorted,
paged delegation inventory for the effective Hub boot/epoch. Pending delivery
includes new-epoch commands even though the original epoch's row is closed. Each
Hub FULL consumer validates original payload hashes and follows retained adjacent
funded handoffs before writing permanent denial tombstones. Closing can stop
issuance during recovery but does not bypass finite ACK requirements for spending.
It changes no original financial partition and preserves exact receipt replay.

Every immutable page must have an authenticated, digest-matching FULL receipt;
the complete delegation list, page sequence/count, effective epoch/boot and
original generation must match before QueueHandoff can create a CAS command.
An old admission closure, missing binding/page/receipt, epoch column or unknown
response remains insufficient. CAS enqueue retains occupation; only actual Node
FULL CAS proof releases the proven capacity difference. Original issued/unknown
credit remains held. Repeated PG recovery producers and a later cold recovery of
an already transferred generation remain separate pending work. CR-008 remains
unapproved and Phase6 remains8/11.


## Repeated PostgreSQL same-store recovery producers (candidate)

A later adjacent recovery derives its FromEpoch roots through every committed
prior admission and exact Node FULL fence/root witness. The original allocation,
delegation digest, source, generation, limits, raw parent and deadlines stay
unchanged. Original installation receipts remain pinned to their original Hub
boot. The initial Node provision is checked independently against original roots;
the new inventory fence uses the proven effective FromEpoch root. Each next root
revision requires its own authenticated FULL fence and receipt before admission.

Complete prefix verification accepts exact registered historical issue bytes only
at their own proven epoch/boot/sequence, and requires every registered issue from
all epochs of each original linked allocation. Missing intermediate admission,
unknown source history, missing earlier issue or a different cold parent denies.
Current-state admission validates the effective share issuer while preserving the
original closed row and original generation. It appends a new immutable admission
and binds only the explicit next boot; local finite FULL ACK remains separate.
Repeated service/store cycles do not by themselves complete platform orchestration,
later cold recovery or production RuntimeServer convergence. CR-008 is unapproved.

## Cold closure of transferred generations (candidate)

A committed platform cold-recovery inventory can close a generation that passed
through one or more same-store admissions. Closure validates canonical proof
bytes/digest, both bound incarnations and request columns, the exact original
share row, and a trusted lower time bound past the complete original hard
boundary. This proof closes issuance only; it does not grant renewal to the cold
boot, transfer old credit, settle unknown usage or release financial occupation.

The original renewal-authority row retains its first admission closure forever.
The independent cold proof is consumed when preparing closure and again before
binding the immutable Node CAS command. Unacknowledged live-closure operations
may be retired as failed in the same audit transaction; no Hub receipt is
manufactured. Capacity stays held until the actual Node FULL CAS receipt proves
the new generation. Replays retain the same cold proof and CAS bytes.

This closes the service-level cold path for transferred generations. Automatic
platform recovery orchestration, effective funding/refill and RuntimeServer
convergence remain separate unfinished work. No wire field, migration or approved
snapshot changes in this unit. CR-008 remains unapproved.

## Explicit request-scoped recovery orchestration (candidate)

`AdvanceHubSameStoreRecovery` consumes an existing explicit platform approval
under the current authenticated platform Grant scope. It never derives a role
from approval/task CreatedBy. Target-tenant scopes preserve the same authenticated
principal and independently authorize the same action; an existing read/mutation
scope cannot become Grant.

The coordinator audits its approval/finite-manifest inventory read, then creates
at most32 missing commands per call. Each command retains the existing independent
audit/task/Outbox transaction. Existing workers deliver committed Node fences and
roots. Missing manifest, fence receipt or root receipt yields its corresponding
waiting stage. All original admission guards run again before PG admission.
A later call resumes from immutable PG evidence; concurrent calls cannot duplicate
commands or manufacture receipts. `admitted` describes PG evidence only, including
historical replay; local Hub FULL admission and finite ACK remain independent.

This is a resumable explicitly authorized service action for the06-07 HTTP
adapter. It is not a background worker grant and does not automatically approve
signer authority, cold recovery or new platform actions. No public HTTP endpoint,
wire field, migration or approved snapshot is introduced here. CR-008 is unapproved.
