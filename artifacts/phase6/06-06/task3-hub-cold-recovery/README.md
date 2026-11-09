# Explicit cold Hub boot recovery — development evidence

Base source: f41ff501. These are development working-tree results, not clean-source Docker or paired GitHub Actions acceptance.

`RecoverColdHubBoot` requires explicit platform Grant authority and a canonical immutable request naming both boots/epochs and reason. It serializes with signer approval, new shares and funding under the global Hub-incarnation lock. Before binding a newer preapproved epoch, the trusted lower UTC bound must prove all old tenants' authority deadlines and original delegation/share hard boundaries expired. Each old tenant must also have current approval for the successor epoch. The entire ordered authority/share inventory and allocation payload hashes are stored immutably in the same audit/task/Outbox transaction as the new boot binding.

This is cold recovery after every hard boundary, not early S/R recovery. Unknown original funds, source projections, settlements, capacity and child grants are unchanged. Late original usage remains acceptable and charges once to its original source. A fresh empty Hub can reconnect over TLS1.3 after the explicit recovery; it receives no runtime or credit authority merely for connecting. Old boots stay denied. HTTP exposure, production successor recompile/reprovision, same-store S/R and Node boot recovery are still pending.

New SQLite may restart local journal numbering. Issue receipts now include the authenticated original Hub boot; issue, seal and runtime-lease sequence uniqueness includes Hub boot while all prior immutable facts remain. Candidate migration8 is not frozen. New native/Linux databases were created and migrated for this unit; previously applied candidate8 databases were not silently upgraded.

Evidence:
- focused-native.log: platform/scope, lower-bound expiry, shared audit rollback, exact proof replay/immutability, all-tenant deadline and successor approvals, eight competing target boots and retired epoch negatives.
- focused-mtls-native.log: same checks plus actual separate-role TLS1.3 fresh-store connection after proof and old-boot denial.
- focused-late-usage-native.log retains a compile failure from mistakenly using UnknownUsageBatch's OriginBootId on UsageBatch. focused-late-usage-native-corrected.log passes the final unknown-parent and late-original-usage extension.
- full-backend-native-race.log: complete backend race passed (service168.658s), before the final extra late-usage assertion; that final assertion has focused native/Linux coverage.
- linux-race-build.log: focused actual mTLS and complete Linux arm64 service/control/worker/timeauthority race (service165.274s), followed by successful Backend/Hub linux amd64+arm64 cross-build command. Cross-build is not native amd64 execution.
- linux-final-late-usage.log: final focused tests including late usage.
- generation.json: pinned sqlc v1.31.1 generated21 files twice in independent temporary directories; both match the working generated files exactly.

Tests use controlled trusted clocks to examine hour-scale authority boundaries; they do not claim an actual hour-long outage or Node runtime activation after recovery. The existing real60s lease test remains part of the complete backend race. No proto field/tag or previously approved snapshot changed.

Phase6 remains8/11. No06-06 SUMMARY and no CR-008 approval is implied.
