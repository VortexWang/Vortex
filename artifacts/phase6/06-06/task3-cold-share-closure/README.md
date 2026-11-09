# Cold Hub proof closes old renewal — development evidence

Base source: 3d54de81. Working-tree checks do not constitute clean-source Docker or a paired Actions acceptance.

The production ShareRenewalClosureWorker consumes the immutable platform cold-recovery record for the exact original predecessor share. It verifies the proof hash, original share identity and all original fields, current bound successor incarnation, and the trusted lower time bound. Existing live-closure work cannot hide an eligible cold-proof candidate. Cold closure retires unacknowledged page operations as failed and locally consumes their Outbox, retaining immutable commands and any previously accepted receipts. No Hub receipt is invented. The closure, work retirement, task/Outbox and audit commit together.

A prepared successor epoch share is not a retired old right. Cold recovery now excludes those shares from its old inventory while preserving their independent hard boundaries. Old-generation capacity stays held until actual original-boot SQLite FULL rate and connection CAS and matching PG receipts release only the capacity difference. Child financial debt remains unchanged.

The regression seeds original generation1 commands in real Node SQLite, prepares generation2, queues live old-Hub closure work before recovery, records platform cold recovery, exercises audit rollback and eight concurrent production closure workers, then performs actual Node FULL CAS and authenticated service receipt consumption. It checks no fabricated Hub receipts, no orphaned unacknowledged live tasks, and exact capacity release. Finally it independently prepares a fresh rule and billing snapshot for epoch8, reserves a fresh parent, FULL-issues from fresh Hub SQLite and accepts its ledger sequence1 alongside original-boot sequence1. Exact old replay still succeeds, while relabeling it as the new boot fails. Both original and new103-byte parent holds remain (F206, charged0).

Tests use controlled trusted clocks for hour-scale expiry. Rule Prepare receipts use the existing authenticated fixture boundary; this test does not claim actual Engine activation of the new rule. Node aggregate CAS and Hub issue are actual FULL SQLite consumers. The original pending rule keeps its configuration/port holds. Same-store S/R, Node boot recovery and original-rule execution convergence are not completed by this unit.

Development evidence:
- focused-native.log: initial fixture correctly rejected an expired platform grant. The fixture now extends the grant through the real versioned platform service.
- focused-native-corrected.log: cold proof/aggregate CAS regression passes before the fresh-rule addition.
- focused-refunding-native.log, focused-new-revision-native.log and focused-recompile-diagnostic.log: old pending Prepare prevents port/configuration transfer to another deployment. These failures are preserved; no execution state was forced active. The final fresh-rule test uses a separate explicitly reserved port.
- focused-fresh-rule-native.log: final full focused regression passes.
- disconnected-consumer-red.log: final valid fixture fails when the cold-proof consumer is deliberately disconnected; the original consumer was restored before all final checks.
- full-backend-native-race.log: complete native backend race.
- linux-race-build.log: Linux arm64 service/control/worker/timeauthority race, followed by Backend/Hub linux amd64+arm64 cross-builds. Cross-build does not prove native amd64 execution.
- generation.json: pinned sqlc v1.31.1 independently generates21 files twice, matching the working generated files.

Phase6 remains8/11. Full06-06 producer and exact CR-008 review remain pending; no06-06 SUMMARY or approval is implied.
