# 06-06 Task3 persistent authenticated child-close consumer

Candidate migration8 adds immutable per-child commands and accepted final
receipts. Commands derive only from an accepted complete Hub delegation seal
and its original issue bytes. Command, operation, task, Outbox and audit share
one PG transaction. Concurrent producers replay one immutable command. A worker
is wired into the production backend; direct NodeControl independently checks
credit-close-v1 and pinned Node boot before delivery.

Authenticated final consumption verifies command digest, exact original grant
identity (except local ShareRevision), raw conservation, explicit version1 known
inventory and original canonical wire payloads. PG reconstructs sorted batch
IDs/payload hashes and child actual totals; unknown liability, disputes, missing
batches and historical missing inventory remain pending. Final receipt, task,
Outbox and audit commit atomically. This unit accepts child evidence only; the
parent's issued hold and aggregate fractional accounting remain unchanged.

- focused-native.log: complete seal gate, eight concurrent producers, immutable
  histories, command/final audit rollback, original scope mutation, unsigned
  receipt, missing inventory/PG batches, known37-byte settlement at1200 and
  parent unknown retention.
- transport-native.log: all focused tests plus actual TLS1.3 worker capability
  isolation, pending ticket -> unsettled, returned ticket, lost FULL final,
  reconnect replay, PG delayed batch -> accepted final and completed operation.
- full-backend-native-race.log and full-root-native-race.log: full native suites.
- linux-race-build.log: fresh candidate DB migration, Linux arm64 backend
  service/control/worker/timeauthority and root component race; backend/Node
  amd64+arm64 builds. amd64 is compilation evidence only.
- generation.json: two independent clean sqlc runs match all19 generated files.

Development evidence does not replace clean-source or paired Actions acceptance.
Complete all-child parent release, sustained funding/lease delivery, explicit
boot recovery, full06-06 producer and exact CR-008 proposal remain pending.
Candidate migration8 is unfrozen; previous candidate databases are not silently
upgraded. Prior approved contracts remain unchanged. Phase6 remains8/11 and no
06-06 SUMMARY is produced; CR-007 does not approve CR-008.
