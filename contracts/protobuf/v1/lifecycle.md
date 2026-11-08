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
