# 06-06 continuation: Task 3 bounded execution units

Task 1 GREEN b2d4079b. Task 2 RED 33b177d3, GREEN 606bc8f1.
Task 2 covers authenticated direct Node capacity CAS, FULL SQLite receipt/floor rollback, actual runtime aggregate gates, PostgreSQL uncertainty retention, lost receipts/SIGKILL/recovery, and capability isolation. Development Docker evidence is not a clean committed source or Actions pair.

Task 3 exceeds the original estimate; split implementation into the following dependent, reviewable units under the existing plan, without claiming plan completion:

1. Deterministic integer distribution and durable 60-second candidate windows/queues. Candidates derive from desired enabled revisions and persisted successful Prepare, current grants and immutable compiler/billing inputs; deduplicate tenant/node references. Property tests assert exact conservation and stable remainder ordering.
2. Bind every platform runtime reference to the existing node aggregate, verify per-rule limits and independent runtime/version/billing/share/autonomy deadlines. Production compiler/orchestrator may not issue Commit before bootstrap reservation. Preserve self-hosted exemption.
3. Authenticated, funded Hub allocation and runtime lease producer with deployment trusted clock and signer role root. Epoch/autonomy survive replay/restart; new boot remains blocked until durable closure and stop/deadline evidence. No test may replace blocked status with active absent this consumer.
4. Full required race/fault checks, generation/compatibility and clean Docker producer; construct exact CR-008 candidate and immutable prior-approval protection. CR-007 approval does not approve CR-008. Final same-source Actions remains 06-11.

Phase 6 remains 8/11. No 06-06 SUMMARY until all units and its exact contract approval gate pass.
