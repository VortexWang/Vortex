---
phase: 05
plan: '13'
status: verified
plan_complete: true
updated: 2026-11-06
---

# 05-13 share lineage and timed policy integration

Within approved authorization 1.2.0 and control 1.3.0, Agent persists immutable policy snapshots/transitions and configuration-owned share revision holds in additive FULL transactions. Engine remains database-free. Prepare, Commit and each new OPEN validate installed complete lineage. Historical revisions remain charged while configuration activation or existing sessions retain occupancy; only proven release clears configuration holds.

New session selection uses the trusted Earliest/Latest interval, effective_at and configurable guard. Existing sessions retain their original snapshot/source and original absolute authorization limits. Future periods require a separately installed finite delegation and exact future authorization family; preissue never creates funds. Same-family renewals remain eligible without admitting unrelated grants. Missing future funding, revoked transition, stale payload and untrusted time deny new admission. Signed authorizations, source anchors and backend allocation in the process tracer remain explicit test fixtures, not Phase 6 production authority.

Native named TestSharePolicyProcess scenarios passed for policy-cutover, policy-future-revoked, policy-hub-restart and five hostile share variants (parent, issuer, cycle, capacity, revision). Real TLS/HELLO and TCP/UDP OPEN are exercised; missing runtime readiness is a failure. Guard denies new streams while existing streams continue; old/new session ledger snapshot IDs stay separate; revocation and late replay cannot restore admission. Retained unresolved 512-byte liability is never released. Store tests additionally cover unknown activation occupancy, proof-backed release, CAS, future-family renewal, missing funds, clock uncertainty, exact replay and atomic migration/write failure. Trigger injection models transaction failure, not physical power loss.

Independent policy audit inspects six consistent Agent/Hub database snapshots, hashes, immutable coefficient bindings, actual session snapshot references, separate funded debits, share holds and unknown debt. VACUUM INTO copies are consistent backups whose journal mode may be delete; metadata separately records the running source WAL mode.

Same committed-source Docker/Actions full gates and final independent audits are pending. This incremental evidence does not complete the plan or phase. Unrelated Phase 4/05-03/wave2 edits are preserved.

## Final paired verdict

Source `bc1c262602e03a8da544d72d071ae27874fb3701` passed clean pinned Go1.27.3 Docker Linux arm64 and Ubuntu24.04 amd64 Actions [42204137919](https://github.com/VortexWang/Vortex/actions/runs/42204137919). Actions deployment lifecycle also passed. Retained current outputs, pair.json and files.sha256 are under artifacts/phase5/check/bc1c262602e03a8da544d72d071ae27874fb3701/. Earlier pending statements are superseded by this final result.

Both hosts passed all eight required actual-process cases and eight named Store/race groups, full root/Engine regression, isolated disk-full, regional failover, credential rotation, drain/upgrade, recovering and conservation checks. Independently rerun Actions audits passed policy six snapshots, recovery five snapshots/ten faults, renewal twelve snapshots and settled retention. Archive13496817615 selection retained128 current members and omitted3051 historical/binary members; archive hash and member selection are recorded. Darwin full root, Agent/Hub race, Engine runtime/share race and focused vet also passed; Linux paired evidence supplies required platform semantics.

Review caught an overly narrow exact-ID filter: future-period new-session admission now accepts only validated renewals of that same funded family, retaining source/parent/issuer/epoch/snapshot and absolute bounds; unrelated grants remain excluded. TestPolicyFutureRenewalKeepsFundedFamily protects this. Existing per-session Gate interface already supports immutable snapshot IDs, so no Engine source/SQL or normative contract modification was needed. Additive policy tables retain schema16 compatibility via a version marker; upgrade regressions pass. Bounded selection retains historical data without scanning unlimited future policy rows.

T-05-23/24 and the scoped G05-05 share/policy portion are covered by current process/negative/occupancy/write-failure evidence. Full 34-requirement dispositions, threat review, missing/forged-evidence tests and renewed owner UAT remain05-14. Phase5 is not complete. The exploratory05-14 Python discovery failed because its planned tests/phase5 directory does not exist yet; it is not counted as a pass and will be implemented by05-14.
