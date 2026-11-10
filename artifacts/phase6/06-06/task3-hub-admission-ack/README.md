# 06-06 Task3: independent admission transport and atomic local finite FULL ACK

Development checkout on base `acf26002`. Not a clean-source Docker/Actions pair,
full06-06 completion or native amd64 execution. CR-008 remains unapproved and
Phase6 remains8/11.

`hub-sr-admission-v1` and typed backend tag20 are independent of forensic preflight,
ordinary funded ACK and heartbeat. Only an already committed explicit platform
admission can produce the canonical authenticated capability. The server checks
current boot/epoch/all-tenant approvals and original deadlines; sorted Node and
tenant locks stabilize current Node assignment/boot/floor, tenant eligibility and
active share bindings. The transaction writes an audit event before returning.
It creates no approval, financial mutation, operation or local ACK.

The local SQLite FULL consumer pins backend,Hub,actual boot,recovery,epochs and
the complete original manifest/prefix hashes. It reads every row through S+1,
including confirmed rows; rows must be contiguous with valid hashes and the exact
final handoff. One transaction ACKs only the finite prefix, completes the open
recovery, clamps original deadline, records fresh authenticated liveness/online
transition and appends an immutable admission receipt. Exact replay is clock-free
and returns original bytes without retiming. This explicit recovery capability can
refresh still-recovering liveness past ordinary30s heartbeat grace, while ordinary
heartbeat/offline/expired/closed guards stay unchanged. Later tail stays pending;
usage settlement, financial R/debt and original shares never change.

Production Hub opts into separate preflight+admission. A lost response before FULL
cannot confirm the prefix. A lost funded status after FULL reconnects without a
second transfer or fresh funding. An admitted boot is now issuer evidence even
before its first new-epoch issue; empty Hub DBs cannot pass old/fresh blocked funded
bootstrap or adopt a recovery with missing original local manifest.

Coverage includes:

- FULL-trigger rollback,8 concurrent callers over2 SQLite connections,reopen,
  exact no-clock/retimed-delivery replay,unchanged original financial audit,
  immutable receipt,finite tail retention,43-row prefix and SIGKILL before/after.
- Backend/boot/epoch/manifest/prefix/sequence/signer-state/time negatives,missing
  or rehashed rows,ordinary heartbeat gap refusal; wire backend/Hub/boot/session/
  region/digest/unknown-field isolation.
- Real independent backend/Hub role-mTLS, forged Node-role certificate denial,
  unapproved retention refusal,empty DB legacy/new-client refusal,PG admission
  delivery loss,local FULL followed by funded-status loss,reconnect and original
  R issue(37 old+10 new raw against the same103 parent). Unresolved tail stays
  pending and all original financial/capacity/settlement rows are unchanged.
- Service current scope,Node boot/disabled/fencing,tenant restriction,newer
  authority,expiry,unapproved/unsigned/wrong boot/backend and audit failure.
- Deliberately removing only complete-prefix hash equality via a temporary Go
  overlay makes the rehashed-row test detect acceptance(expected RED). Normal
  source passes; before/after source hashes match.

Initial failures retained:

- `local-focused-native.log`: test expected a bare usage ID; actual immutable
  journal ID is Node-prefixed. Corrected assertion, no production guard change.
- `service-focused-native.log`: unused test import(build failure), corrected.
- `root-linux-race.log`: parallel full component race hit the existing bounded
  `TestRegionalFailoverRetainsUnknownDebt/recovering`5s timeout under race load.
  Store181.163s,Hub25.404s,contracts/NodeCLI passed; Agent failed. The isolated
  complete failover test and serial full component rerun are recorded separately
  in `root-linux-serial-race.log`; original failure remains failed.

Native complete root race: Agent65.052s,Store94.221s,Hub16.497s,contracts5.116s,
NodeCLI4.495s. `backend-native-race.log` precedes the last assignment/authority
tightening. `backend-native-final-race.log` includes Node disabled/assignment
checks but precedes only the final all-tenant authority equality check; final
focused native tests include that final guard. Final Linux complete backend and
serial root results are recorded in the manifest after process completion.
Two isolated23-output sqlc and2-output protobuf generations match the worktree;
six Backend/Hub/Node amd64/arm64 cross-builds are separate from target execution.

Remaining: explicit platform recovery orchestration,end-to-end repeated PG
recovery producers,generation-level new-epoch renewal/closure,pending-rule
convergence,full06-06 producer and exact CR-008 review,then06-11 paired acceptance.

Generation closure safety refinement: admission closes an old epoch, not all
future issuers of the same share generation. `ShareHasAdmittedRecovery` now makes
QueueHandoff fail closed for recovered predecessors until the separate effective
epoch closure protocol is implemented. The regression prepares a legitimate
smaller successor and proves no CAS/task/Outbox/capacity change can use the old
closure proof. Initial test fixture500-bit/s burst choices violated the existing
minimum/burst limits (`generation-closure-guard-native.log` and
`generation-closure-guard-corrected-native.log`); corrected300000-bit/s/37500-byte
fixture reaches the intended gate and passes. Original production limits stay
unchanged. This refinement required final complete backend reruns:
`backend-complete-final-native.log` and `backend-complete-final-linux.log`.
`generators-repro-final.json` and `cross-builds-complete-final.json` cover final
query/guard code. Prior build/generator artifacts precede this refinement.

Final audit: contract changes are limited to current candidate lifecycle/proto
files; approved snapshots stay unchanged (`approved-snapshot-protection.json`).
The generation CAS refinement is a temporary fail-closed boundary, not a claim
that recovered generation handoff/renewal is complete. Final source hashes are
recorded independently; historical failed checks retain their original results.

Final complete-code backend race passed: native service263.908s and Linux arm64 service270.720s (`backend-complete-final-*.log`), including final all-tenant authority and generation closure safety gates. Final root serial Linux race passed Agent98.331s/Store158.497s/Hub20.189s/contracts1.017s/NodeCLI2.370s. Earlier backend runs are snapshots of the refinements described above.
