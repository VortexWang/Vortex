---
phase: 05
status: reviewed_with_findings
depth: standard
reviewed_sha: 71502ce37e110aef3af7e1549c39dbf35d8be19c
review_mode: inline-owner-request
---

# Phase 5 code review

Scope: Agent, Hub protocol fixture, SQLite store, Engine integration, node entry point, upgrade installer and five plan summaries. Review performed inline, as requested; no independent agent review is claimed.

| ID | Severity | Finding and evidence | Disposition |
|---|---|---|---|
| R1 | Critical | `hub/testhub.go:Connect` trusted PeerCertificates without requiring a verified chain. A TLS 1.3 listener configured with RequestClientCert allowed an unrelated CA certificate to acquire a durable control receipt. | Fixed in 71502ce. Real-wire red/green regression `agent/control_security_test.go#TestControlHubRejectsUnverifiedPeerChain`; now requires VerifiedChains before any receipt. |
| R2 | Warning | `cmd/vortex-node/main.go` calls single-address `ControlClient.Run`; `ConnectRegion` is not wired, has no per-endpoint deadline, and has no online preference. A stalled candidate can prevent failover. | Open G05-01. |
| R3 | Warning | Upgrade installer and drain/health/snapshot hooks exist, but the node entry point never invokes the installer. No service-level drain/force-close/replace/restart/rollback demonstration exists. | Open G05-02. |
| R4 | Warning | Agent control certificate renewal is wired; Engine runtime loads its own identity once. No verified common rotation/revocation path covers both control and VLink admission. | Open G05-03. |
| R5 | Warning | `hub/enrollment.go:Serve/handle` has no connection concurrency cap, request byte cap or read deadline. An authenticated-server bootstrap endpoint can retain goroutines or consume memory with incomplete/oversized requests. | Resolved f6a5891 with four real network regressions, repeated race and same-SHA Docker/Actions 42140165239; protocol fixture scope. |
| R6 | Warning | No Agent probe/report scheduler or complete periodic health/version cache exists. Existing Engine health helpers and one-shot control heartbeat do not prove these behaviors. | Open G05-01. |
| R7 | Info | Three 05-02 structured coverage references named removed tests; newer tests cover the corresponding bounded slice. | Corrected references with an explicit audit annotation. |
| R8 | Warning | S/R transaction/replay tests pass, but runtime recovery does not expose a distinct recovering-vs-caught-up operational mode and the full share-reference/policy-transition orchestration is absent. | Open G05-05. Production backend authority stays Phase 6 under D-19. |

The five plan summaries establish their implemented slices, not all 34 mapped requirements. No unimplemented feature is treated as a passing manual check. Existing unrelated Phase 4 changes were excluded.
