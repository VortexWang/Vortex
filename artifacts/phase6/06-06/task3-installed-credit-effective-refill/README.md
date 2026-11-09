# 06-06 Task3: installed credit final/refill after same-store recovery

Development checkout on base0f450b34. Phase6 remains8/11,06-06 incomplete and
CR-008 unprepared/unapproved. No clean-source same-SHA Docker/Actions claim.

The installed-credit fixture exposed a genuine finalization gap: an authenticated
complete seal accepted at effective epoch8 was later rejected because parent
final demanded the original allocation boot7. FinalizeHubAllocation now locks
the global Hub before tenant/accounting and independently derives the exact seal
epoch/boot through every original admitted S/R chain,retained manifest and Node
FULL fence/root witness. Seal row columns/ID/digest/authenticated peer must agree.
No expiry/current-epoch shortcut or changed source/coefficient is accepted.

The shared real PG/Hub/Node SQLite flow covers both zero-child and installed old
credit. The installed case FULL-installs51-byte epoch7 credit,records37 actual
known bytes locally,then explicitly completes S/R7->8 finite manifest,actual Node
FULL fence/roots,PG admission and local Hub FULL admission. Actual effective Hub
seal retains the issued debt. PG known usage plus authenticated original Node
FULL child final still cannot refill until dedicated parent final. Final settles
37 original-source bytes with no outstanding hold,then8 concurrent funders create
one fresh epoch8 parent. Original shares/artifacts/admission closure remain exact,
new real Hub issue is accepted,original share renewal stays valid and repeated
attempts cannot refill the fresh unfinalized parent.

Deliberately corrupted admission,manifest and Node root-receipt digest in isolated
test databases refuse parent final without accounting/task/audit changes. Failed
parent-final audit rolls back everything;8 concurrent exact finals settle once.
Existing general coefficient/remainder,unknown,child inventory and refill tests
execute alongside focused tests. focused-native.log is retained expected RED for
old immutable boot comparison. green-native.log and focused-complete-native.log
pass after independent effective-chain validation.

Four Backend/Hub linuxamd64/arm64 builds pass; final complete native/Linux backend
race outcomes are recorded in manifest.json. No SQL/protobuf input,migration,
root Agent/Hub source,approved snapshot,public API or dependency changes. Earlier
generation/root evidence is inherited unchanged. Linux execution isarm64,not
nativeamd64. These are real service/store tests,not a new network Engine recovery
acceptance. RuntimeServer/pending-rule convergence,complete06-06 producer/fault/
clean-source checks and concreteCR-008 review remain.06-07 HTTP and06-11 pairing
remain pending.

Final complete backend native race passes(service315.385s); Linux arm64 race passes(service331.626s). Four Backend/Hub cross-builds pass.
