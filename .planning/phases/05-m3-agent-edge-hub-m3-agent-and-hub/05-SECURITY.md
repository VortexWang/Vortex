---
phase: "05"
slug: m3-agent-edge-hub-m3-agent-and-hub
status: verified
threats_open: 0
threats_open_total: 1
asvs_level: 1
register_authored_at_plan_time: false
audit_mode: retroactive-stride-inline
reviewed_sha: 71502ce37e110aef3af7e1549c39dbf35d8be19c
created: "2026-11-06"
---

# Phase 5 security

This is a retroactive STRIDE review of the implemented protocol-test slice, not production Hub certification or an ASVS certification. Configured blocking threshold is high. One medium threat remains open; zero blocking threats does not satisfy the broader phase acceptance criteria.

## Trust Boundaries

| Boundary | Data and control |
|---|---|
| Bootstrap peer → enrollment fixture | One-use token, CSR, node identity; server-authenticated TLS |
| Node → Hub control stream | mTLS node/boot identity, configuration, grant, usage and receipts |
| Agent → independent Engine | Config fencing, authorization and durable reservation callbacks |
| Agent/Hub → separate local SQLite files | FULL commit, outbox, epochs, activation holds and unknown debt |
| Hub fixture → protocol backend | Revocation ordering, prefix/tail ACK and S/R proof; production authority belongs Phase 6 |
| Upgrade artifact → local executable/state | Signature, digest, schema compatibility, drain and rollback |

## Threat Register

| Threat ID | STRIDE category | Component | Severity | Disposition | Mitigation / evidence | Status |
|---|---|---|---|---|---|---|
| T-05-01 | Spoofing | Control peer | high | mitigate | Require TLS 1.3 VerifiedChains and SAN/node binding; `TestControlHubRejectsUnverifiedPeerChain`, normal mTLS and mismatch tests; fix 71502ce | closed |
| T-05-02 | Tampering | Authorization / ACK | high | mitigate | Hash-bound immutable payloads and source/boot/fencing validation; `TestUnknownLiabilitySurvivesBadAckLostAckAndRestart`, epoch and runtime-authority tests | closed within implemented slice |
| T-05-03 | Repudiation | Usage / S/R replay | high | mitigate | FULL outbox and contiguous hash-bound checkpoint; `TestRecoveryReplaysDurablePrefixAndTailAfterLostACK`, independent snapshot audit | closed within implemented slice |
| T-05-04 | Information disclosure | Private key | high | mitigate | Local CSR key generation, restrictive permissions and credential validation; registration and identity tests | closed within implemented slice |
| T-05-05 | Denial of service | Enrollment fixture | medium | mitigate | No enforced request/deadline/concurrency bounds in `hub/enrollment.go`; G05-04 | open — below high threshold |
| T-05-06 | Elevation of privilege | Old epoch / old boot | high | mitigate | Settlement-only old boot, one successor and R cap; authenticated epoch/restart tests and SQLite audit | closed within implemented slice |
| T-05-07 | Tampering | Upgrade artifact | high | mitigate | Installer rejects signature/digest/schema mismatch; installer tests. Service integration remains G05-02 and is not certified by these tests. | closed at library boundary |
| T-05-08 | Denial of service / tampering | Disk failure and unknown execution | high | mitigate | Fail closed before unrecordable writes, retain unknown holds; real tmpfs full-disk and SIGKILL tests, no timeout-based debt release | closed within tested fault model |

## Accepted Risks Log

No accepted risks. Open medium threat remains actionable; it was not silently waived. Physical power-loss durability and production backend security are not proven by these process/fixture tests.

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|---|---|---|---|---|
| 2026-11-06 | 8 | 7 | 1 medium / 0 blocking | inline GSD audit |

## Sign-Off

- [x] All threats have a disposition.
- [x] No owner risk acceptance fabricated.
- [x] `threats_open: 0` at configured high threshold.
- [x] Retroactive register and remaining medium issue recorded.

Approval: security threshold verified for the implemented slice; phase acceptance remains `gaps_found`.
