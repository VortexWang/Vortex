# Credit expiry closure and funded runtime successor — development evidence

Base source: 8200038c. This working-tree evidence is not a clean committed-source Docker result or a paired GitHub Actions acceptance.

The Hub scanner now seals a fully issued delegation before its longer parent deadline only when the trusted lower bound proves every immutable original child credit expired. It verifies payload hashes, Node/boot/source, positive amounts and exact partition conservation. An unissued balance or unexpired child prevents early seal. Expiry alone releases no issued funds.

The actual TLS1.3 test runs separate backend/Hub/Node role roots and the production transport/worker consumers. A short original child TTL exercises automatic seal, missing-PG-inventory retention, authenticated Node final, original parent/source settlement (37 raw at1200 =>44 charged plus400 milli remainder), one funded successor, direct source provisioning and actual Agent runtime readiness with both rate and connection aggregate caps. Old credit is unchanged. This short-TTL fixture does not replace the separate real60s lease and original300s bootstrap tests.

Passing checks:
- expiry-green.log: race-enabled focused Store expiry and existing seal tests; expiry-red.log records the initial real failing expiry test.
- root-components-native-race.log: complete Agent/Store/Hub/contracts/Node CLI race on native Darwin arm64.
- full-backend-native-race.log: complete native backend race; service166.690s. The final aggregate-cap fixture enhancement is additionally covered by focused native and Linux runs.
- actual-cycle-clock-corrected-native.log: continuous live connection cycle after correcting fixture clock.
- actual-cycle-aggregate-native.log and linux-cycle-aggregate.log: final fixture including actual Node aggregate CAS records.
- linux-cycle-components-race-build-corrected.log: Linux arm64 actual cycle, root component race and successful Node/Hub Linux amd64+arm64 cross-build command (exit0). Cross-build is not execution on native amd64.

Earlier actual-cycle logs and linux-cycle-components-race-build.log retain failed development attempts. Initial fixed5s wait was too short; later fixture advanced the backend clock4s into the Node future, so Node correctly rejected status. The final fixture uses real continuous time and a bounded wait, without changing production clock/recovery guards. One install-diagnostic run passed with a receipt wait; the final test does not require that wait.

No SQL/protobuf generation input changed in this unit. Phase6 stays8/11; explicit boot/epoch recovery and full06-06 producer remain. CR-008 is unprepared/unapproved; no06-06 SUMMARY.
