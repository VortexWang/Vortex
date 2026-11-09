# 06-06 Task3 safe runtime funding successor

Automatic funding now requires every matching predecessor to have a dedicated
complete original-parent final, or an accepted zero-child seal final. Expiry,
exhaustion, conservative estimates and missing receipts never qualify. A stable
sorted allocation/reservation inventory binds the successor nonce, with a second
check under the tenant transaction lock before any new reservation. Initial
bootstrap nonces remain compatible. Each successor has a fresh source-funded
parent, retaining source-local availability, coefficient and aggregate remainder.

- focused-native.log: bootstrap/CAS gates, explicit-allocation dedup, unclosed
  parent refusal, complete parent final, failed-audit rollback, eight concurrent
  workers, one fresh successor, actual FULL Hub issue without another reserve,
  and repeated unclosed-successor ticks.
- atomic-audit-native.log: strengthened rollback comparison includes financial
  rows plus tasks, operations and Outbox.
- full-backend-native-race.log: full native backend race passed.
- linux-race-build.log: Linux arm64 service/control/worker/timeauthority race
  passed, with backend linux/amd64 and linux/arm64 cross-builds. Compilation
  does not establish native amd64 execution.
- generation.json: two independent sqlc v1.31.1 generations match all19 outputs.

Development evidence only. Root code is unchanged. Sustained60s runtime/share
lease refresh, explicit boot/epoch recovery, full06-06 producer and exact CR-008
proposal remain pending. CR-007 approval does not approve CR-008; Phase6 stays
8/11 and no06-06 SUMMARY is generated. Final Actions pairing belongs to06-11.
