# Node original-credit inventory fence — development evidence

Base source: 82532d9c. Phase6 remains8/11;06-06 Task3 incomplete; CR-008 unapproved. This unit adds only an internal Agent FULL-store primitive. It adds no NodeControl message, capability, authenticated transport, PG receipt producer/consumer, source-root producer or production S/R admission path.

The motivating rollback gap is real: a Hub can have issued a credit that was installed at Node but never yet reported to PG. A PG prefix and partition alone cannot tell that the old Hub snapshot omitted it. RecoveryCreditFence therefore binds backend/recovery/manifest/sealed sequence, actual Node identity/boot, original Hub parent/source/snapshot, exact original credit root, adjacent signer epochs, original raw budget, complete expected credit ID/amount inventory and issued/R. The exact original root must match FULL local bytes, payload hash and backend source owner; the local source epoch must equal from_epoch. Every existing credit row for that source must match the independent expected inventory and original parent/issuer/Node boot/snapshot/rule/path. Missing expected credits are not converted into spendable R. A committed fence blocks delayed old-epoch credit; new credit/handoff must match the original recovery ID, manifest SHA, sequence and exact R. New-epoch issue remains bounded by both R and original raw budget even if the mutable handoff remainder is corrupted. Existing old rights and unknown usage stay held.

Command, receipt and local admission fence commit atomically. Exact replay after reopen/clock expiry returns the original receipt. The command creates no authorization and changes no Engine state, clock, global floors, money or capacity. A cold Node cannot prove an old local inventory; it remains on cold/deadline recovery. The primitive is not wired for production use.

Evidence:

- focused-native.log and focused-final-native.log: basic inventory/rollback, original source accounting, old-child refusal, handoff binding and replay PASS.
- focused-complete-native.log: unknown debt preservation, independent SQLite connection floor race, immutability and complete focused suite PASS.
- focused-race-native.log: four independent-connection races between a late child and fence; exactly one atomic outcome each run, PASS.
- focused-linux.log: Linux arm64 focused Node FULL/race tests PASS.
- root-native-race.log: complete native Agent/Store/Hub/contracts/Node CLI race PASS.
- root-linux-race-retry.log: complete Linux arm64 component race PASS. An earlier concurrent full-root/full-backend run hit TestRegionalFailoverRetainsUnknownDebt/autonomous's six-second context while the host was saturated. The isolated Linux subtest passed in1.979s; the serial complete root retry passed Agent85.504s, Store120.371s, Hub16.722s, contracts1.023s and Node CLI2.413s. This is recorded as a resource-contention fixture timeout, not silently discarded.
- backend-native-race.log: complete native backend race with real PG/Valkey PASS; backend code was unchanged by this local Agent increment.
- linux-race.log: complete Linux component/backend race with the same concurrent-load Agent fixture timeout; component Store/Hub/contracts/Node packages passed. The independent serial full-root retry is the final Linux Agent evidence; the fresh-DB Linux backend service/control/worker/timeauthority race passed separately in task3-hub-sr-prefix-proof.
- cross-builds.log: Backend, Hub and Node linux amd64/arm64 builds and ELF identities PASS; not native amd64 execution.

This remains development checkout evidence, not clean-source or same-SHA Actions acceptance. Production direct-backend authentication, PG immutable command and Node receipt, next roots, runtime S/R transport/admission, effective new-epoch issue/lease/seal, original pending-rule convergence, full06-06 producer and exact CR-008 proposal remain. Final Actions pairing belongs to06-11; no06-06 SUMMARY is generated.
