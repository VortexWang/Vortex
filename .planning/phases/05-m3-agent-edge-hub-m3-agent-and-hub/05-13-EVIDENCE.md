---
phase: 05
plan: '13'
status: incremental
plan_complete: false
updated: 2026-11-06
---

# 05-13 share lineage and timed policy integration

Within approved authorization 1.2.0 and control 1.3.0, Agent persists immutable policy snapshots/transitions and configuration-owned share revision holds in additive FULL transactions. Engine remains database-free. Prepare, Commit and each new OPEN validate installed complete lineage. Historical revisions remain charged while configuration activation or existing sessions retain occupancy; only proven release clears configuration holds.

New session selection uses the trusted Earliest/Latest interval, effective_at and configurable guard. Existing sessions retain their original snapshot/source and original absolute authorization limits. Future periods require a separately installed finite delegation and exact future authorization family; preissue never creates funds. Same-family renewals remain eligible without admitting unrelated grants. Missing future funding, revoked transition, stale payload and untrusted time deny new admission. Signed authorizations, source anchors and backend allocation in the process tracer remain explicit test fixtures, not Phase 6 production authority.

Native named TestSharePolicyProcess scenarios passed for policy-cutover, policy-future-revoked, policy-hub-restart and five hostile share variants (parent, issuer, cycle, capacity, revision). Real TLS/HELLO and TCP/UDP OPEN are exercised; missing runtime readiness is a failure. Guard denies new streams while existing streams continue; old/new session ledger snapshot IDs stay separate; revocation and late replay cannot restore admission. Retained unresolved 512-byte liability is never released. Store tests additionally cover unknown activation occupancy, proof-backed release, CAS, future-family renewal, missing funds, clock uncertainty, exact replay and atomic migration/write failure. Trigger injection models transaction failure, not physical power loss.

Independent policy audit inspects six consistent Agent/Hub database snapshots, hashes, immutable coefficient bindings, actual session snapshot references, separate funded debits, share holds and unknown debt. VACUUM INTO copies are consistent backups whose journal mode may be delete; metadata separately records the running source WAL mode.

Same committed-source Docker/Actions full gates and final independent audits are pending. This incremental evidence does not complete the plan or phase. Unrelated Phase 4/05-03/wave2 edits are preserved.
