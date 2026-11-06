---
status: resolved
trigger: Phase 5 security T-05-05 / G05-04
execution: inline-owner-preference
---

# Enrollment resource bounds

Confirmed source evidence: EnrollmentServer spawns unlimited handlers, decodes unbounded JSON, and neither closes its listener on context cancellation nor bounds stalled TLS/read/write operations. Existing claim "bounded test/bootstrap endpoint" is unsupported.

Hypothesis: a valid oversized registration consumes a one-time token; idle connections retain handlers indefinitely. Add real TLS regressions for rejected oversized registration without token consumption, bounded stalled requests/concurrency and cancellation cleanup. Fix only the protocol fixture boundary; production token lifecycle remains Phase 6.

Red: `TestEnrollmentRejectsOversizeWithoutConsumingToken` failed before the fix: oversized registration accepted and token consumed.

Fix: hard 64 KiB decoded-JSON envelope limit before token redemption; default 64 admitted connections and five-second deadline spanning TLS/read/write; immediate overload close; context cancellation closes listener and all accepted handlers, and Serve waits for cleanup. Limits are configured before serving. No contract schema or wire fields changed.

Green: four real TCP/TLS tests cover oversized rejection with successful same-token retry, incomplete-request timeout, pre-handshake overload and cancellation of stalled TLS. Ten race repetitions and full Agent/Hub race tests pass locally. Same-SHA Linux Docker/Actions evidence follows the source commit before security closure is finalized.
