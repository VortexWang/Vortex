# 06-07 bounded execution

06-06 exact CR-008 approved in4dc91a55. Phase6 is9/11;06-07 and06-11 incomplete.

Task1 is split into atomic units under the existing plan:
1. PostgreSQL opaque access/rotating refresh token families, hash-only storage, current principal lookup, reuse revocation, audit rollback/concurrency.
2. Resource-oriented Gin REST adapters, stable safe errors, signed scope/principal cursors and If-Match/operation_id.
3. Public contract candidate and exact copy/coverage plus recovery administrative adapters.
Task2 remains committed domain events, ordered durable cursor/replay, retention resync and SSE. Final same-SHA Docker/Actions pairing remains06-11. No06-07 SUMMARY until full plan completion and exact public contract gate.

Token unit uses migration9(candidate, not frozen) and SQL-first generated access. Trusted Issue is only a post-authentication adapter entrypoint; no public actor/role-selecting login is exposed. Access TTL is configurable up to15min; family TTL up to30d and never slides on refresh. Every access resolves current PG user/platform role/state and tenant state. Refresh family row locks ensure one rotation; reuse commits an immutable family revocation in the same audited transaction. Audit failure rolls back issue/consume/revoke/newtoken writes. Raw access/refresh material never enters DB or audit.

Complete native/Linux auth race tests and existing policy/repository regressions pass; two independent24-output sqlc generations match. Initial missing implementation/build errors are not semantic RED. A deliberate missing-revocation mutation fails the real family-access assertion; its overlapping Linux compile also saw that mutation and is retained as invalid passing evidence, not a production regression. Restored source final native/Linux race pass. Current unit does not claim HTTP authentication, password login, SSE or complete06-07 acceptance.
