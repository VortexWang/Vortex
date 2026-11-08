# 06-06 Task3 funding-to-aggregate generation dependencies

Candidate development evidence on Go1.27.3, native Darwin arm64 and Docker Linux
arm64 with isolated PostgreSQL18.6 databases. Not completed06-06 or final Phase6
acceptance. CR-007 approval does not approve candidate CR-008.

Every new funded allocation stores two immutable dependencies on the actual rate
and connection aggregate generations, atomically with parent reservation, audit,
task and Outbox. A fresh delegated source UUID starts source-reference generation1;
that lineage number cannot serve as either Node aggregate generation. Renewal
closure snapshots use exact kind/generation/epoch/boots; incomplete bindings stay
unknown and cannot produce an empty closure proof. Candidate migration00008 is
not yet frozen; no silent repair of an older applied candidate DB is attempted.

- `backend-final-focused-race.log`: real Node CAS advances connections1->2 while
  rate remains1. Funding persists connections2/rate1; the actual FULL Hub issue
  maps to that allocation. The next connections2 closure excludes historical
  connections1, while rate1 closure includes funding from both connection eras.
  Funding audit failure rolls back both dependencies and all authority records;
  dependency mutation is rejected. A deliberately incomplete historical fixture
  cannot commit closure/tasks or release holds. Existing65-delegation pagination
  and actual Node CAS-only release regression also pass.
- `backend-all-race.log`: complete native backend race suite, fresh isolated DB.
- `linux-race-build.log`: explicit first migration, Linux arm64 service/control/
  worker/timeauthority race, backend Linux amd64/arm64 builds. Cross-building
  amd64 is not native execution/performance evidence.
- `generators.json`: two independent clean sqlc v1.31.1 runs match all18 outputs.
  Protobuf has no input or generated changes in this unit.
- `backend-focused-race.log` covers the earlier initial focused run; final focused
  log also includes the missing-binding failure boundary. `migrate-native.log`
  records explicit initial migration before parallel test package processes.

The missing-binding test intentionally removes one immutable row only inside its
isolated fixture to model incomplete historical proof; production exposes no such
mutation. It does not replace a blocked signer/Node status with an active status.
No delegation/child final stop, sustained refill or boot/epoch recovery is inferred.
The SDK's historical Phase5 `passed_with_scope` state rejection remains preserved
in the adjacent delegation-seal evidence; only current Phase6 continuity changes.
Phase6 remains8/11. Final complete producer/contract approval and same-SHA Actions
pairing remain pending.
