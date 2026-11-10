# 06-06 Task3 complete issued-child parent final

A separate production worker revalidates the accepted complete Hub issuer seal,
every original child command and authenticated FULL final, and independently
rebuilds all committed wire inventories before settling the one original parent.
Node tuple/funded-usage/source locks serialize ingestion and unknown recording.
Missing children, unknown liabilities, disputes or inconsistent aggregate usage
remain held. Neither expiry nor a generic proof can substitute for this path.

Final settlement uses the original source, coefficient and aggregate remainder;
no per-child rounding or second financial reserve occurs. Stable proof binds the
seal digest and sorted child receipt digest/amount inventory. Final ledger,
resource task, Outbox and audit share one PG transaction. Concurrent/restarted
workers replay one settlement without repeating financial or resource events.

- focused-native.log: missing seal/child denial, mixed37-byte installed and
  never-installed child, failed-audit rollback, eight concurrent finalizers,
  immutable replay, two tiny1-byte children at multiplier1 (charge0/remainder2),
  unknown created after accepted child, plus actual TLS pending/lost FULL final/
  reconnect/delayed-PG-batch through actual parent settlement.
- full-backend-native-race.log: complete native backend race.
- linux-race-build.log: Linux arm64 service/control/worker/timeauthority race and
  backend amd64+arm64 builds. amd64 is compilation evidence only.
- generation.json: two independent clean sqlc runs match all19 outputs.

All checks passed. Root code did not change from the preceding complete native
and Linux root race. These are development results, not clean-source or paired
Actions acceptance. Unknown remains conservative; this does not implement boot
recovery, sustained lease delivery or refill. Those, full06-06 producer and exact
CR-008 proposal remain pending. Prior approved contracts remain unchanged;
CR-007 does not approve CR-008. Phase6 remains8/11; no06-06 SUMMARY.
