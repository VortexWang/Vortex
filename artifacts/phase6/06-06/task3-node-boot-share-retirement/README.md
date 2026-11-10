# Node cold-boot share retirement primitive — development evidence

Base source: 9ecaaa03. This local prerequisite is not wired into production NodeControl and does not constitute complete Node boot recovery. PostgreSQL complete authority proof, explicit platform approval, independently negotiated direct-backend transport and authenticated backend receipt consumption remain pending.

A strict canonical candidate command pins backend/tenant/Node/old/new boot, original independently numbered rate and connection commands, greater fencing floor, bounded issue/expiry/safe-after times and authority-proof digest. Node trusted lower time must prove the command and every original hard deadline; any later old-boot authorization already in SQLite independently prevents early retirement. SQLite FULL commits both zero-capacity retired heads and immutable deterministic receipt together. It creates no old execution receipts, grants, release/final, trusted heartbeat or activation-stop proof. Fresh disk retains rate7/connections3 floors; retained disk must match exact old heads. A new-boot CAS must independently advance each kind before capacity is enabled. Exact retry after expiry/reopen/later CAS returns original bytes without lowering the active successor. Corrupt receipt replay fails canonical reconstruction. Different new boots on independent SQLite connections cannot split ownership.

Evidence:
- focused-native.log: fresh/retained disk, FULL rollback, per-kind floors, denied pre-CAS capacity, reopen/exact replay, actual next FULL CAS, wrong boot/backend/Node/clock/original/floor and cross-connection race.
- focused-complete-native.log: adds canonical contract boundaries and a later local grant hard deadline; old256-byte credit remains unchanged after retirement.
- root-components-native-race.log: complete native Agent/Store/Hub/contracts/Node CLI race before the final canonical receipt reconstruction hardening.
- focused-receipt-canonical-native.log: focused check after receipt hardening.
- final-store-native-race.log: complete Store race after receipt hardening.
- final-focused-corruption-native.log: final explicit changed-command, immutable SQL receipt and injected stored-byte-corruption negatives. The corruption test was added after final-store build; production source is unchanged.
- linux-components-integration-build.log: Linux arm64 Agent/Store/Hub/contracts/Node CLI race, focused backend cold-recovery/expiry role-mTLS regressions and Node/Hub linux amd64+arm64 cross-builds. Root compilation began before the receipt hardening; the subsequent backend integration compiles the final production source.
- linux-final-receipt-build.log: focused Linux final receipt implementation plus Node dual-architecture cross-build.

Cross-build is not native amd64 execution. These are development working-tree checks, not clean-source Docker or paired Actions acceptance. No SQL/protobuf generator input changed in this local unit. Previously approved snapshots remain unchanged; candidate contract is not CR-008 approval. Phase6 stays8/11; no06-06 SUMMARY.
