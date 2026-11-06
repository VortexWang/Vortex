---
phase: "05"
slug: m3-agent-edge-hub-m3-agent-and-hub
status: validated
nyquist_compliant: false
wave_0_complete: false
created: "2026-11-06"
---

# Phase 5 validation audit

Reconstructed from five plans, current test files and current ROADMAP/CONTEXT. Status validated means the coverage audit ran; it does not mean all requirements passed.

## Test Infrastructure

Go testing/race/vet, Python contract and snapshot checks, SQLite FULL/WAL, isolated 4 MiB Linux tmpfs. Quick: `go test ./agent/... ./hub/...`. Full: `make phase5-check` in pinned Go 1.27.3/protoc 36.2 environment. Docker uses `sh tools/phase5/docker_check.sh`; Actions phase5.yml executes the same full entry. Full feedback is several minutes; no watch flags.

## Per-Task Verification Map

| Task group | Requirements | Threat | Command / actual evidence | Coverage |
|---|---|---|---|---|
| 05-01 durable store / node | AGT-003, SUP-AGENT-001 | T-05-08 | `go test ./agent/store/... ./agent/...`; node lifecycle tests | PARTIAL: command/service interface absent |
| 05-02 enrollment/control/upgrade | AGT-001/002/008/012 | T-05-01/04/07 | `go test -race ./agent/... ./hub/...`; registration, identity, installer tests | PARTIAL: real service/failover/VLink identity gaps |
| 05-05 control apply / installer | AGT-004/008/010 | T-05-02/07 | control_apply and installer fault tests | PARTIAL: library boundaries covered, service integration absent |
| 05-03 grants/usage/autonomy | AGT-005/006/007/011, HUB-001..009 | T-05-02/03/08 | authorization gate, source conservation, unknown usage, autonomy and real full-disk regressions | PARTIAL: probes/reporting/remote renewal absent |
| 05-04 activation and S/R | AGT-009/010, HUB-010..018, SUP-B1/B3/B4 | T-05-03/06/08 | recovery-race.log and five audited snapshots via full gate | PARTIAL: recovery mode/share-policy runtime orchestration incomplete |
| Security regression | HUB-001 | T-05-01 | `go test -race ./agent -run TestControlHubRejectsUnverifiedPeerChain` | GREEN: real unrelated-CA refusal before receipt |

Full 34-ID map and exact limits: `05-REQUIREMENT-AUDIT.md`. Historical 05-02 stale test references were corrected with an annotation; no non-existing test is counted.

## Wave 0 / gap dependencies

G05-01/02/03/05 now have gap-mode plans 05-06..05-14 grounded in the existing audit and current source; implementation and matching integration tests remain pending. Existing test infrastructure is available. Missing behaviors are not reclassified as manual-only checks or accepted exceptions. Runtime failover, service replacement and credential rotation must be demonstrated against actual processes before acceptance.

## Manual-Only Verifications

Owner judgment of the reported recovery/security boundaries and operational acceptance occurs in 05-UAT.md. Physical power loss is outside the demonstrated process-failure model; no manual result is fabricated.

## Validation Sign-Off

- [x] Current test infrastructure and five plan groups mapped.
- [x] 34 phase requirement IDs accounted for.
- [x] No watch-mode commands.
- [ ] Missing runtime integrations and new negative tests completed.
- [ ] `nyquist_compliant: true` — intentionally false pending G05-01/02/03/05 implementation and evidence.

Approval: audit complete, coverage partial; phase advancement not approved.

## Security follow-up

G05-04 now resolved: `hub/enrollment_security_test.go` adds four real network tests; ten repeated race runs and same-SHA f6a5891 Docker/Actions pass. Remaining G05-01/02/03/05 implementation gaps keep Nyquist partial.

## Gap-plan verification map (planned, not executed)

Planning checks do not change `nyquist_compliant: false`, current requirement status or historical UAT. New tests/scripts below must be created by their owning tasks; names are future outputs, not current green evidence. Every named process case must execute with assertions; empty selection or SKIP fails acceptance.

| Plan | Planned verification | Required negative boundary | State |
|---|---|---|---|
| 05-06 | candidate lifecycle Python fixtures; contracts_check.sh; Go contract round-trip | unapproved bytes, overflow, incompatible identity/version | Pending implementation and exact proposal approval |
| 05-07 | TestRegionalProcessFailover + regional race regressions | stalled Hub, late old stream, no healthy peer | Pending |
| 05-08 | TestSchedulerProcessRenewal + store/Hub race regressions | duplicate/lost response, exhaustion, revocation, disk/time failure | Pending |
| 05-09 | TestIdentityProcessRotation + Engine/Agent race checks | revoked/expired/wrong SAN, mixed signer, publication crash | Pending |
| 05-10 | TestNodeCommandsDrain + Docker signals/systemd unit verification | established mux new OPEN during drain, deadline, unauthorized admin | Pending |
| 05-11 | TestUpgradeProcessRollback + installer race checks | bad signature, stop unknown, health failure, financial rollback | Pending |
| 05-12 | TestRecoveringProcessCatchup + recovery race checks | forged ACK, crash, >1000 records, continuous tail | Pending |
| 05-13 | TestSharePolicyProcess + Engine/share and store race checks | stale CAS, missing/cyclic parent, unknown clock, period cutover | Pending |
| 05-14 | mandatory process gate, independent snapshot verifier, same-SHA Docker/Actions | missing/wrong SHA/skip/empty/forged evidence | Pending |

New threats T-05-09..26 are planning entries, not closed audit findings. Full verification must update review/security/requirement dispositions and collect renewed UAT for new behavior. The 68 spec-less fallback edges remain explicit backstops; lack of evidence stays unverified.
