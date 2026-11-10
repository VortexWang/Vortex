# 06-06 Task3: effective share-generation renewal and closure

Base52f66c56, development checkout. Phase6 stays8/11;06-06 incomplete and CR-008
unprepared/unapproved. These are native Darwin arm64 and Docker Linux arm64
results, plus cross-builds; no clean-source/Actions pair or native amd64 execution.

An original generation and its closed old-epoch renewal row are never reopened.
The backend derives current issuance from every adjacent immutable admission,
its exact original generation, original allocation bindings, complete manifests
and Node FULL fence/root witnesses. Fresh renewal checks current Node boot,
floor, eligibility, signer approval and original deadlines. Exact nonce replay
returns original bytes. Global Hub locks precede Node and tenant serialization.

Preparing successor closure freezes sorted64-item pages for the effective
Hub boot/epoch, with task/Outbox/audit atomicity. A closed original row no longer
suppresses new-epoch delivery. The Hub FULL consumer validates original payload
hash and retained funded handoff chain before denial tombstones, preserving
original funds/remaining/issued and exact prior receipt replay. It may close
issuance while the finite recovery ACK is pending, without enabling spending.

QueueHandoff requires every immutable page, digest-matching authenticated FULL
receipt and the exact complete effective delegation set. Old admission closure
alone, missing binding/page/receipt or an epoch column is insufficient. Enqueue
keeps capacity; actual Node FULL CAS releases only the proven difference. Tests
show600000->300000 rate capacity and unchanged original financial/closure proof.
A separate actual backend/Hub role-mTLS production client exercises lost FULL
receipt and lost PG ACK reconnect, denial after local closure and later CAS queue.
The local consumer also closes after two retained epochs and covers FULL rollback,
eight callers/two SQLite connections, reopen, original issue replay and complete
original512-issued/512-unissued seal partition.

Recovery detection comes from immutable admission Shares rather than an allocation
binding join: a missing dependency must not erase the fact that an epoch transferred.
Every linked allocation must retain both bindings; removed candidate-fixture
binding denies renewal, closure preparation and CAS without state changes.

Expected RED deliberately substitutes the original closed proof for effective
closure using a temporary Go overlay. The existing old-epoch CAS regression
correctly fails. Working source before/after hashes match. Restored focused tests
pass. No approved snapshot, protobuf tag, dependency or migration changed.

Retained failures/development snapshots:

- local-focused.log: original issue API requires trusted time even for replay;
  the fixture used an empty clock. Missing-handoff corruption also hit its FK.
  Corrected fixtures use unchanged valid clock and corrupt retained from_epoch;
  local-focused-corrected.log passes. Production time/FK rules were not weakened.
- backend-focused.log: new-epoch page delivery was suppressed by the original
  closed row. Fixed actual SQL eligibility; backend-focused-corrected.log passes.
- backend-transport.log/diagnostic.log/corrected.log: fixture first used a stale
  fixed heartbeat, then changed only the backend clock. Diagnostic assertions
  now require traversal of injected loss; both sides use bounded advancing time.
  backend-transport-final.log passes with actual FULL/PG loss.
- backend-final-focused.log: a tightening initially queried snake_case JSON keys
  while generated PG structs marshal PascalCase. Missing-binding fixture also
  sent two parameterized SQL statements together. These were corrected; no
  production guards were removed. backend-final-scope-corrected.log passes.
- backend-complete-scope.log preceded the corrected admission JSON query.
- backend-native-race.log(service312.373s)/backend-linux-race.log(service337.296s)
  passed before final dependency-proof detection tightening. Final full backend
  reruns are separate backend-final-*-race.log results, recorded in manifest.
- Native root final production race passed Agent57.908/Store84.777/Hub13.028/
  contracts1.509/NodeCLI2.500s. Linux serial root passed Agent86.945/Store190.650/
  Hub20.552/contracts1.020/NodeCLI2.229s. Later changes were backend-only.
- generators-repro.json/cross-builds.json precede final JSON-key correction;
  *-final.json records independent final generators/builds.

Remaining: repeated PG recovery producers, production recovery orchestration,
subsequent cold recovery of transferred generations, effective funding/refill,
RuntimeServer/pending-rule convergence, full06-06 producer/clean-source/fault
verification and concrete CR-008 review; REST06-07 and paired acceptance06-11.

Final complete backend race passed on the final source: native service280.487s and Linux arm64 service290.487s. Final independent sqlc23-output generation and six cross-builds match this source. Final scope-focused suite(service21.818s) covers the missing-binding tightening. Original expected RED overlay source SHA equals final source SHA.
