# 06-06 Task3 runtime/share lease refresh and independent heartbeat

A separate FULL Hub journal emits a runtime/share pair every conservative60s,
with at most300s TTL and the original allocation/root/boot/epoch/path/fence/limit
boundaries. It never issues or extends credit. An independent10s empty envelope
persists authenticated heartbeat evidence through the existing Node path, without
weakening the30s recovery guard. Status remains observation only. Both streams
are immutable, replayable and independent of credit inventories and finances.
PG authenticates the Hub, original allocation, roots and initial issue, enforces
both cadences, and commits its separate receipt with audit/task/Outbox atomically.
Only the new separately negotiated capability accepts this journal; prior3/4/5
capability and usage-only branches reject it over actual role-specific mTLS.

Final verification:
- focused-store-complete.log: FULL failure rollback, replay/reopen, concurrent
  refresh, scope/root/clock/closure boundaries, independent heartbeats, unchanged
  credit seal inventory and same-session lease replacement preserving credit.
- focused-backend-complete.log: initial issue prerequisite, independent cadences,
  eight concurrent consumers, financial invariance, shared audit rollback and
  wrong scope/credit/source/limit/revocation/renewal negatives.
- full-backend-final-native-race.log: complete native backend race PASS, including
  the actual60s Agent FULL install and lost FULL receipt/PG ACK replay test with
  all four legacy-capability rejection cases. Service package174.927s.
- root-components-final-native.log: final native Agent/Store/Hub/contracts/CLI
  race PASS.
- linux-schema-initialize.log: isolated fresh Linux database migrated before
  concurrent package execution; includes candidate8 runtime lease receipt table.
- linux-race-build-final.log: Linux arm64 service/control/worker/clock race PASS
  (service169.612s), then build failed due to mistyped ./cmd/backend path.
- linux-components-build-corrected.log: corrected ./cmd/vortex-backend path;
  Backend/Hub/Node linux/amd64+arm64 cross-builds and final Linux arm64
  Agent/Store/Hub/contracts/CLI race PASS. Cross-build is not native amd64 testing.
- generation.json: two independent pinned sqlc generations match all20 outputs.
- schema-projection.log: bounded candidate capability projection checks only.

Earlier failures remain raw evidence: focused-backend.log jumped62s without
intermediate heartbeat and correctly hit recovery; corrected fixture passed.
actual-mtls-native.log used an insufficient5s waiter. The corrected waiter then
exposed the real30s heartbeat continuity guard in actual-mtls-native-corrected.log.
The guard was preserved; independent10s FULL heartbeats fixed delivery, proven in
actual-mtls-native-heartbeat.log and the final full native/Linux suites. Earlier
focused/corrected/component logs precede the final independent-heartbeat and
cadence changes and do not replace final results.

Development working-tree evidence based on aeb11f86; not clean-source Docker or
same-SHA Actions acceptance. Candidate migration8 is unfrozen; a database already
at an earlier candidate8 is not silently upgraded. No approved snapshot changed.
CR-007 is approved; CR-008 remains unprepared/unapproved. Phase6 stays8/11;
no06-06 SUMMARY. Next: sustained credit closure/refill through expiry, explicit
Hub/Node boot/epoch recovery, complete06-06 producer and exact CR-008 review.
Final paired Docker/Actions acceptance belongs to06-11.
