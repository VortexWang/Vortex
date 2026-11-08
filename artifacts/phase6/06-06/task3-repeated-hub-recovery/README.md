# 06-06 Task3: repeated PostgreSQL same-store recovery

Development checkout on base4f2e351e. Phase6 stays8/11,06-06 incomplete and
CR-008 unprepared/unapproved. No clean-source Docker/Actions pair is claimed.

Approval, credit-fence/root producers, current-state admission and retained
financial-issue verification now derive original allocations through each already
committed adjacent admission. Original payload/delegation hashes and installation
receipt stay pinned to the original boot/bytes. Only effective root epoch/revision
advance; all source IDs, generations, limits, amount and deadlines remain fixed.
An unrelated cold/older parent with no admitted chain still cannot be adopted.

The initial Node provision is independently validated against original roots
before deriving the current effective fence root. Each later source provision
requires its exact new FULL inventory fence. Historical issue receipts retain
original boot/sequence/payload and are checked against their own admitted epoch;
new records still require current binding. MissingHubSameStoreIssues now includes
all epochs of each original linked allocation, alongside the immediate old boot,
so earlier-boot registered credit cannot disappear from the finite prefix.

Three actual SQLite/PG recovery cycles (7->8->9->10) retain a single103-byte
parent with37 old plus10 at each new epoch(67 total issued). Validated original
and new credit records are installed into the Node store; every later FULL fence
checks the whole historical credit inventory. Roots advance to revision4 while
preserving original bounds. Each cycle completes complete prefix retention,
independent verification, Node FULL fence/roots, concurrent PG admission, explicit
local FULL admission, issue receipt and generation renewal. Original financial,
capacity/allocation/provision rows never change. Three-epoch effective closure
then permits CAS enqueue only after its authenticated FULL receipt.

Repeated approval audit rollback and concurrent exact producers/admissions pass.
Unadmitted intermediate ownership cannot be skipped. A deliberately deleted old
retained journal row remains detectable from the earlier-boot PG issue inventory;
verification refuses without changing authority/tasks/money. Historical approval
commands replay byte-for-byte after later recoveries.

This unit exercises producer/consumer services and real SQLite/PG; its repeated
cycles do not add a new network test. Existing production role-mTLS tests execute
in full backend suites. Agent/Hub wire source and root modules are unchanged;
root race evidence from4f2e351e is inherited unchanged, not rerun or relabeled.
Backend/Hub linuxamd64/arm64 cross-builds are compilation, not native target tests.
Two isolated sqlc23-output generations match the worktree; no migration,
protobuf tag, approved snapshot, API or dependency changes in this unit.

Failures retained:

- bootstrap-regression.log and repeated-native.log failed to compile because
  sqlc inferred ID instead of RecoveryID when the same positional parameter
  reached the recovery table. Explicit sqlc.arg(recovery_id) fixes the generated
  API; repeated-native-corrected.log passes.
- repeated-complete-native.log used tenant-only authoritySnapshot on the global
  hub_signer_incarnations table. Corrected fixture excludes that table; production
  gates unchanged. repeated-complete-corrected-native.log passes(service6.243s).
- repeated-installed-native.log separately passes the real installed Node-credit
  variant(service4.005s), before additional negative fixture assertions.

Final full backend native/Linux race outcomes are in manifest.json. Remaining:
explicit platform orchestration, later cold recovery of transferred generations,
effective funding/refill, production RuntimeServer/pending-rule convergence,
complete06-06 producer/fault/clean-source checks and exact CR-008 review;06-07 HTTP
and06-11 same-SHA Docker/Actions acceptance remain pending.

Final complete native backend race passed(service281.814s); complete Linux arm64 backend race passed(service297.312s). Independent23-output sqlc generations and four Backend/Hub cross-builds passed on final source.
