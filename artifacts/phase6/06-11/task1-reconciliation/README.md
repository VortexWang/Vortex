# 06-11 Task1 finite backend reconciliation

Real PostgreSQL production service/lease/receipt/audit/outbox paths over1000 rules and2000 fixed members; deterministic Node query adapter, not actual1000 Engine/network execution. New worker drains32ready items per batch and waits only when idle, while a current authorization rollback returns no-work to avoid spinning.

- native-before-drain.log/json: genuine RED,60s/300rules/600members; failed. Commit field empty because this was uncommitted development, not clean-source acceptance.
- native-drain-race.log/json: qualification PASS,35.673607917s/1000/2000; service total198.961s includes setup. Development identity only.
- linux-drain-race.log/json: qualification case PASS21.082300572s/1000/2000; overall package FAIL for shared oldDB compile-worker backlog. Do not classify the whole log as passed.
- native-related-race.log: FAIL from shared oldDB backlog left by historical/development fixtures (original interrupted fixture also retained).
- native-related-fresh-race.log: affected complete selected regression PASS, service6.048s.
- linux-related-fresh-race.log: affected selected regression PASS, service5.427s.
- fixture-interrupted.log: first development run intentionally interrupted after discovering it used a shared fixture and a nonexistent share_heads snapshot name; isolated fixture/share_chains correction preceded the real RED.

Report status/timing/partial-count negative cases and worker drain/cancellation/idle/failure tests pass. Final guard marks invalid timing reports failed before persistence. No migration, SQL, generated file, dependency or approved normative bytes changed. Clean-source normal/race, full fault and same-SHA paired acceptance remain Task2; no06-11 SUMMARY yet.
