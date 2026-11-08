# 06-06 Task3: explicit authenticated recovery orchestration

Development checkout on base2d98ef5e. Phase6 remains8/11,06-06 incomplete and
CR-008 unprepared/unapproved. No clean-source same-SHA Docker/Actions claim.

The service consumes an existing explicit recovery approval under the caller's
current authenticated platform Grant scope. No task/approval CreatedBy becomes
a principal or platform role. ForTenant preserves the actual principal and
requires its existing action before independently authorizing the target tenant.
Owner,operator,auditor,zero scope and read->Grant escalation are denied.

The audited coordinator validates canonical immutable command/manifest inventory,
exact owner/backend/links and current pending authority. At most32 missing Node
fence/root commands are created per request. Each uses its existing independent
AuditWriter/task/Outbox transaction. Existing workers transmit after commit.
Missing proof gives awaiting_hub_manifest/awaiting_node_fences/awaiting_node_roots.
Every actual Node FULL fence/root receipt is required before existing all-tenant
AdmitHubSameStore checks run again. admitted means committed PG evidence only;
Hub local ACK/online readiness and unknown/capacity/money closure remain separate.

Real PG+Hub/Node SQLite tests progress each stage, reject unauthorized scope and
wrong backend, roll back failed inventory audit, run8 concurrent producers and
admissions, keep actual caller attribution, retain original financial/capacity
rows and leave the local Hub blocked until its own FULL admission.33 actual
allocations produce32 then1 fence with no starvation/duplicate or unproved roots.
Expiry,untrusted time,Node boot/account change,new epoch,wrong tenant and failed
admission audit refuse without authority/task/Outbox/boot changes. The inventory
read audit may commit before a later independent step fails; the coordinator
does not claim one transaction across asynchronous stages.

red-native.log is the expected initial compile RED for the missing service API.
green-native.log passes the first actual proof flow. focused-complete-native.log
adds principal/budget checks; focused-final-native.log adds all current guards.
This unit adds a request-scoped service for the06-07 authenticated HTTP adapter;
it is not a background authorization grant or a new endpoint, and does not add
a new network acceptance test. Existing role-mTLS tests run in full backend suites.

No SQL/protobuf input, migration, root Agent/Hub source, approved snapshot or
dependency changed. Earlier root/generation evidence is inherited unchanged.
Linux container execution isarm64; cross-builds are not nativeamd64 execution.
Final same-source Docker/Actions belongs06-11. Remaining06-06: effective funding/
refill,RuntimeServer/pending-rule convergence and complete producer/fault/clean-
source checks before concreteCR-008 review.06-07 HTTP and06-11 remain pending.

Final complete backend native race passes(service300.164s); Linux arm64 race passes(service314.733s). Four Backend/Hub cross-builds pass.
