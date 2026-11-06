---
phase: "05"
slug: m3-agent-edge-hub-m3-agent-and-hub
status: verified_slice
threats_open: 0
threats_open_total: 0
asvs_level: 1
register_authored_at_plan_time: false
audit_mode: retroactive-stride-inline
reviewed_sha: deb8b08e5cc8ee4e6f77bc700a4b5b36836c68c1
created: "2026-11-06"
---

# Phase 5 security

This is a retroactive STRIDE review of the implemented protocol-test slice, not production Hub certification or an ASVS certification. Configured blocking threshold is high. The medium enrollment resource-exhaustion threat was closed by f6a5891; zero blocking threats does not satisfy the broader phase acceptance criteria.

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
| T-05-05 | Denial of service | Enrollment fixture | medium | mitigate | 64 KiB request cap before token redemption, bounded handlers/deadlines and cancellation cleanup; four real network tests in `hub/enrollment_security_test.go`, repeated race and same-SHA Docker/Actions 42140165239 | closed |
| T-05-06 | Elevation of privilege | Old epoch / old boot | high | mitigate | Settlement-only old boot, one successor and R cap; authenticated epoch/restart tests and SQLite audit | closed within implemented slice |
| T-05-07 | Tampering | Upgrade artifact | high | mitigate | Installer rejects signature/digest/schema mismatch; installer tests. Service integration remains G05-02 and is not certified by these tests. | closed at library boundary |
| T-05-08 | Denial of service / tampering | Disk failure and unknown execution | high | mitigate | Fail closed before unrecordable writes, retain unknown holds; real tmpfs full-disk and SIGKILL tests, no timeout-based debt release | closed within tested fault model |

## Accepted Risks Log

No accepted risks. No open threat was silently waived; T-05-05 was fixed and re-tested. Physical power-loss durability and production backend security are not proven by these process/fixture tests.

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|---|---|---|---|---|
| 2026-11-06 | 8 | 7 | 1 medium / 0 blocking | initial inline GSD audit |
| 2026-11-06 | 8 | 8 | 0 | resource-bound fix f6a5891; paired Linux re-audit |

## Sign-Off

- [x] All threats have a disposition.
- [x] No owner risk acceptance fabricated.
- [x] `threats_open: 0` at configured high threshold.
- [x] Retroactive register and medium issue fixed with retained evidence.

Approval: security threshold verified for the implemented slice; phase acceptance is closed for the Phase 5 runtime/protocol-adapter scope. Production backend security remains Phase 6.

## Gap-plan threat re-audit — deb8b08

The original eight-row register above is historical. New plan-time threats were inspected inline against source and named tests; earlier per-plan paired runs are recorded in their EVIDENCE files. Final consolidated Docker/Actions execution passed on the same source SHA, with independent raw evidence audits. No risk was accepted.

| Threats | Boundary / mitigation inspected | Required current evidence | Disposition |
|---|---|---|---|
| T-05-09/10 | Exact approved lifecycle companions, strict bounds, immutable receipt and source lineage | contracts/lifecycle_test.go; contract hash/approval gate; renewal-journal/share-renewal races | Implemented; paired gate passed |
| T-05-11/12 | Current mTLS incarnation fences mutation; cancel/join old attempts, bounded sweep, retain debt | regional-process.log; regional-race.log; recovery audit | Implemented; paired gate passed |
| T-05-13/14 | Source-scoped idempotent renewal, revocation dominates late response, FULL journal and conservative retention | renewal-process.log; renewal-safety/journal/retention races and raw audits | Implemented; paired gate passed |
| T-05-15/16 | Atomic common identity snapshot, verified SAN/usage/signer; durable revocation, expiry and failed publication | identity-process.log; identity-race.log; identity-engine-race.log | Implemented; paired gate passed |
| T-05-17/18 | Restricted local admin socket, bounded drain, no new OPEN and actual process cleanup | node-commands.log; node-admin-race.log; Docker deployment lifecycle | Implemented; paired gate passed |
| T-05-19/20 | Signed/digest-checked candidate, locked durable journal, witnessed stop, health rollback without financial restore | upgrade-process.log; upgrade-race.log; recovery debt audit | Implemented; paired gate passed |
| T-05-21/22 | Persist finite catch-up target with S/R, contiguous authenticated ACK and independent tail; restart retains barrier | recovering-process.log; recovering-race.log; recovery audit | Implemented; paired gate passed |
| T-05-23/24 | Complete current share chain/CAS and proof-backed holds; immutable separately funded future snapshots, conservative time | policy-process.log; policy-race.log; policy-audit.json | Implemented; paired gate passed |
| T-05-25/26 | Fail-closed source-bound named-case/file/raw-snapshot manifest, no skipped/absent evidence or production-authority claim | gap_check.sh; audit_gap_evidence.py; eleven tests/phase5 negative and WAL fixtures | Implemented; paired gate passed |

No new exploitable high-severity finding was established by this inline re-audit. This register is not an ASVS certification. Final status is verified for the bounded Phase 5 runtime/protocol-adapter scope; Phase 6 production authority and Phase 4 qualification remain explicitly out of scope.
