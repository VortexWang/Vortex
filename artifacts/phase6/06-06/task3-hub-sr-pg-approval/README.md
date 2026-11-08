# Hub same-store recovery: PG approval — development evidence

Base source: d69c95df. Phase6 remains8/11 and06-06 Task3 is not complete. Candidate migration8 is not frozen; CR-008 remains unapproved. New fresh databases vortex_hub_sr_20261108/native and vortex_hub_sr_linux_20261108/Linux include the new approval table; older applied migration8 databases cannot prove this schema.

The producer requires platform Grant and explicit current/target Hub boots, adjacent epochs, independently approved next authority for every prior tenant, authenticated original allocation installation receipts and persisted original backend-owned Node source provisions. It links original backend wire and SQLite delegation hashes separately and advances only each root's revision/epoch. It currently supports original allocations from the immediately preceding epoch; repeated PG transitions still require an accepted effective-authority chain. An immutable command/proof, pending operation, task, Outbox and audit commit together. Exact replay returns old bytes without clock retiming. Competing targets share the Hub incarnation lock and choose one. Preparation neither binds a new incarnation nor ACKs prefix/partition, closes old renewal, releases money/capacity or makes a Hub online.

The PG test executes the generated command against the actual local FULL Hub handoff, then verifies that fresh credit remains blocked behind the unconfirmed prefix. It supplies no fixture ACK. Backend/Hub wire transport, prefix ingestion/verification, new-root direct-Node provisioning and effective new-epoch issue/lease/seal consumers remain pending.

Evidence entries and exact completion/exit status are recorded in manifest.json:

- focused-native.log: initial PG approval, rollback, concurrency, original roots/debt and negative evidence checks. Subsequent test-only refactoring creates missing-evidence fixtures directly instead of deleting immutable rows. Final complete native backend run covers that refactor.
- command-native.log: strict canonical JSON, scope/digest/root/epoch/boot/TTL negatives; same-process same-boot transition allowed only by explicit command.
- focused-linux.log: fresh Linux PG migration and all approval tests, with final fixtures.
- backend-native-race.log: complete native backend race with real PG/Valkey, including approval tests.
- hub-native-race.log: complete native Hub race.
- linux-race.log: native Linux arm64 Hub race, funded local S/R/fault regression and backend service/control/worker/timeauthority race.
- sqlc-repro.log: two isolated sqlc v1.31.1 generations match all23 working-tree outputs. No protobuf tag/generated change.
- cross-builds.log: all six Backend/Hub/Node linuxamd64+arm64 builds and identities; these are compilation evidence, not native amd64 execution.

These are development checkout results, not clean-source acceptance or same-SHA Actions pairing. Original pending-rule convergence, complete production S/R and full06-06 producer remain required. Final Actions pairing belongs to06-11. No06-06 SUMMARY is generated.
