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

G05-01..05 need researched implementation plans and matching integration tests. Existing test infrastructure is available. Missing behaviors are not reclassified as manual-only checks or accepted exceptions. Runtime failover, service replacement and credential rotation must be demonstrated against actual processes before acceptance.

## Manual-Only Verifications

Owner judgment of the reported recovery/security boundaries and operational acceptance occurs in 05-UAT.md. Physical power loss is outside the demonstrated process-failure model; no manual result is fabricated.

## Validation Sign-Off

- [x] Current test infrastructure and five plan groups mapped.
- [x] 34 phase requirement IDs accounted for.
- [x] No watch-mode commands.
- [ ] Missing runtime integrations and new negative tests completed.
- [ ] `nyquist_compliant: true` — intentionally false pending G05-01..05.

Approval: audit complete, coverage partial; phase advancement not approved.

## Security follow-up

G05-04 now resolved: `hub/enrollment_security_test.go` adds four real network tests; ten repeated race runs and same-SHA f6a5891 Docker/Actions pass. Remaining G05-01/02/03/05 implementation gaps keep Nyquist partial.
