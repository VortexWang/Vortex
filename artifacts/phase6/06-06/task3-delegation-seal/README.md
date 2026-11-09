# 06-06 Task3 delegation seal — development evidence

Candidate implementation, not completed06-06 or final Phase6 acceptance. Go1.27.3
darwin/arm64 and Linux arm64 Docker; PostgreSQL18.6 and Valkey9.1.3 are external
test services. The clean f0309cd9 closure regression is recorded separately in
`../task3-renewal-closure/clean-f0309cd9-manifest.json`.

Hub FULL seal permanently stops new issuance, commits the complete sorted issue
inventory digest, and journals the exact never-issued remainder. PG reconstructs
that inventory independently before its audited source-local return. Issued
children and unknown debt stay held; the seal is not proof that a Node stopped.
Three/four-capability funded peers and usage-only peers cannot submit seals.

- `backend-final-focused-race.log`: actual TLS1.3 automatic expiry seal; issue
  records precede seal, lost FULL receipt before PG and lost ACK after PG replay
  without duplicate return. Authenticated old capability branches reject the new
  kind before decoding or acknowledging it. PG inventory mismatch, audit rollback,
  zero-issue final, signed51/unissued52, fractional coefficient1200, already charged
  and delayed actual usage preserve source-local hold and rounding.
- `backend-final-all-race.log`: complete native backend race suite, isolated DB.
- `components-final-all-race.log`: complete native Agent/Store/Hub/contracts/Node
  CLI race. Includes FULL trigger-fault rollback, durable reopen/replay, expiry
  retaining issued debt and concurrent issue/seal conservation.
- `linux-final-race-build.log`: Linux arm64 service/control/worker/clock race,
  focused Store/Hub/contracts/Node CLI race, backend amd64/arm64 builds. The amd64
  cross-build is not native amd64 execution or performance qualification.
- `generators.json`: two independent clean sqlc/protobuf generations match all20
  checkout outputs; input hashes still match the candidate migration/query/proto.

Earlier passing logs cover incremental versions; final logs above cover the final
source. `backend-auto-seal-initial-failed.log` and `linux-race-build.log` retain
the failed11-minute time-jump fixture: it correctly hit the explicit authority
handoff gate before ledger transfer. Automatic expiry now uses a five-second
delegation within heartbeat grace, without weakening production recovery checks.
`*-injection-order-failed.log` retain a negative-case fixture error where the
loss flag was armed before capability tests; it is now armed after those tests.
`backend-compile.log` and `backend-name-shadow-compile-failed.log` preserve initial
compile errors corrected before validation. One initial formatting command used
root-relative paths from backend and did not start tests; corrected commands pass.

SQLite trigger failure proves transactional rollback, not physical power loss.
The GSD session SDK still rejects historical Phase5 `passed_with_scope`; its raw
error is preserved, that historical verdict is unchanged, and only current Phase6
continuity fields are synchronized manually. No child final/unknown release,
sustained automatic refill, explicit boot recovery, complete06-06 producer or
same-SHA Actions acceptance is claimed. CR-007 is approved; CR-008 is not proposed
or approved. Phase6 remains8/11; no06-06 SUMMARY is produced.
