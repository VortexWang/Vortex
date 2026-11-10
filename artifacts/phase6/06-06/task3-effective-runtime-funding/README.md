# 06-06 Task3: effective-epoch runtime funding eligibility

Development checkout on base325db101. Phase6 remains8/11,06-06 incomplete and
CR-008 unprepared/unapproved. No clean-source same-SHA Docker/Actions claim.

Prepared artifacts retain their original epoch. A later effective runtime epoch
requires both active original aggregate kinds to have that original epoch and
complete adjacent admitted S/R proof chains, current bound Hub boot, same Node
boot/path/fence and no pending capacity successor. Cold/new unrelated generations
cannot adopt an old artifact. Advisory scheduling reads issue no authority;
PrepareHubAllocation rechecks under its existing Hub/tenant transaction before
reserving funds. Current boundary/grant/signer/policy/boot/floor/closure guards
remain. Missing authoritative rows return the existing stable pending error.

The original generation limits and deadlines cap the new allocation. Current
source B-F and original billing coefficient fund a distinct parent with fresh
source roots at the effective epoch. Complete cross-epoch predecessor finals
still gate automatic refill; desired-epoch nonce/digest remain deterministic.
The effective issuer validates new source-funded current-epoch allocations bound
to the original admitted generation without expecting them in an older transfer
manifest. Every historical transferred parent retains its exact original proof;
omitted older parents still need a committed seal. Both binding kinds and current
boot/source payload integrity remain required. No original artifact/share/closure
row is rewritten or reopened.

The real PG/Hub/Node SQLite test explicitly completes7->8 finite manifest,actual
Node FULL fence/root receipts,PG admission and local Hub FULL admission. Before
final,refill creates no hold. The actual zero-child Hub FULL seal/PG final then
permits exactly one source-funded epoch8 successor from8 concurrent producers.
A failed final funding audit rolls back reservation/allocation/task/Outbox.
Original shares/artifacts/closure records remain identical. A real new Hub FULL
issue is accepted in PG, original share renewal remains valid after adding that
new parent, and repeated attempts cannot refill its unfinalized credit.
Missing admission/binding,current future epoch,wrong boot,closure barrier and
audit negatives change no authority/financial/tasks/Outbox. Existing independent
kind and recovered share regressions execute alongside the focused tests.

red-native.log is the expected missing effective successor RED. green-native.log
exposed a raw pgx no-rows from another unbound candidate after introducing the new
helper; mapped to the existing stable ErrShareUnknown pending result without
relaxing eligibility. green-corrected-native.log and focused-complete-native.log
pass. The latter includes rollback/concurrency and transfer/current-guard negatives.

No SQL/protobuf input,migration,root Agent/Hub source,approved snapshot,public API
or dependency changes. Earlier generation/root evidence is inherited unchanged.
Linux execution isarm64; four cross-builds do not prove nativeamd64 execution.
This test covers a zero-child predecessor,not installed-child settlement or a
new network RuntimeServer/Engine recovery acceptance. Those and pending-rule
convergence,complete06-06 producer/fault/clean-source checks and concreteCR-008
review remain.06-07 HTTP and06-11 same-SHA pairing pending.

Final complete backend native race passes(service315.137s); Linux arm64 race passes(service337.859s). Four Backend/Hub cross-builds pass.
