# 06-07 public member digest and port prepare/read

Development evidence based on 012dd0d3; no same-SHA clean Docker/Actions or complete06-07 claim.

GET group members now adds only the opaque member_set_version from the same locked configuration as the SQL-whitelisted members. It exactly matches the existing port snapshot digest; health is excluded, incarnation/version changes invalidate it. Ordinary reads use one transaction; platform reads retain fail-closed audit and selected-tenant grant checks.

POST scoped port-reservations validates explicit business inputs and returns202, exact transaction-bound aggregate operation, strong allocation ETag and safe business DTO. GET scoped port-allocations/{id} returns only safe business fields and ETag. No public proof/runtime/boot fields or proof submission endpoint. Prepare can reserve against a preassigned rule UUID, as the existing service permits before compilation; it does not activate that rule. Listen address remains an explicit business input but is not echoed in the response.

Eight concurrent requests for one rule produce one allocation, four held Node/protocol reservations and one aggregate binding. The named active-rule unique conflict maps409; unavailable physical ports return a safe suggested_port. Strict inputs, changed incarnation, auditor denial, foreign-tenant404 and audit rollback of allocations/reservations/operations/tasks/Outbox/events/binding pass. HTTP tests seed observed Node facts only; actual mTLS proof evidence remains the prior service suite, not a new Agent end-to-end claim.

missing-digest-red.log is an actual prior-source Go overlay: the new usability assertion fails because member_set_version is absent; working source unchanged. Native affected service race8.802s, complete API/auth/repository/policy/CLI race7.921s and added incarnation/auditor test1.794s pass. Linux arm64 complete API7.059s and affected service7.324s pass. Four final Backend/Hub linux amd64/arm64 builds pass. No SQL/migration/dependency/frozen contract changes; prior two independent30-file sqlc outputs inherited from012dd0d3. Commit/release/abort HTTP and service aggregate boundaries remain next.
