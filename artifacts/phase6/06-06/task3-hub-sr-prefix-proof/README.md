# Hub same-store retained prefix and independent PG partitions

Base source f34c2677; development checkout only. Fresh candidate8 databases vortex_hub_sr_prefix_20261108/native and vortex_hub_sr_prefix_linux_20261108/Linux contain the two added immutable tables. No approved contract snapshot or protobuf tag changed. Phase6 remains8/11,06-06 Task3 incomplete and CR-008 unapproved.

LedgerPrefix includes confirmed original journal records and checks continuous sequence/payload hashes with count/four-MiB output bounds. Authenticated target-boot RetainHubSameStoreRecord persists exact immutable original bytes/digest with a pending obligation/task/Outbox/audit. Out-of-order retention is allowed; hash retention alone is no completion or settlement.

VerifyHubSameStorePrefix reconstructs sequences1..S and the exact handoff record atS+1, verifies the approved request and prefix hash, matches every prefix issue to its existing canonical PG financial receipt, rejects any current old-boot PG child omitted from the prefix and reconstructs every original allocation issue count/amount/digest and R. An allocation sealed since approval, changed all-tenant authority inventory, advanced epoch/boot, expired command or uncertain clock denies initial verification. Hub-global then sorted all-tenant locks hold the inventory stable. The verified immutable witness and its pending task/Outbox/audit commit atomically. Exact replay is available without retiming after expiry.

Neither API binds an incarnation, confirms local ledger rows, closes old renewal, makes a Hub online, releases funds/capacity or settles usage. Known/unknown bytes remain pending obligations; retained raw journal bytes do not invent missing original usage provenance or a usage ACK. Admission must independently revalidate current authority/seal/financial state, provision Node roots and complete authenticated preflight. No recovery transport handler/capability is implemented here. Repeated effective PG recovery chains and new-epoch issue/lease/seal still remain.

Evidence results and hashes are in manifest.json:

- focused-native.log: first complete PG prefix/partition, rollback, concurrent replay and scope/stale negatives PASS.
- focused-final-native.log: added balanced manifest forgery and42-row cross-page PG inventory PASS.
- focused-inventory-native.log: final complete focused PG tests, including a fully rehashed shorter prefix that omits an independently registered PG child and retained usage that remains unacknowledged/unsettled PASS.
- focused-root-native.log: local confirmed-prefix paging, missing row/corrupt hash, four-MiB output bounds and existing funded S/R/fault regressions PASS.
- focused-linux.log: fresh Linux schema then final service and root focused tests PASS.
- backend-native-race.log: complete native backend race with real PG/Valkey.
- root-native-race.log: complete native Store/Hub race.
- linux-race.log: native Linux arm64 complete Store/Hub race then backend service/control/worker/timeauthority race.
- sqlc-repro.log: two isolated v1.31.1 generations match all23 generated files.
- cross-builds.log: six Linux Backend/Hub/Node amd64+arm64 build hashes and identities; compilation does not prove native amd64 execution.

These are not clean-source or same-SHA Actions acceptance. Production S/R/preflight/direct-Node roots, original pending-rule convergence, full06-06 producer and exact CR-008 proposal remain; final Actions pairing belongs to06-11. No06-06 SUMMARY is generated.
