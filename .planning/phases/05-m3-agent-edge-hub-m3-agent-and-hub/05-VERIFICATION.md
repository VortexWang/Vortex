---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
status: gaps_found
verified: "2026-11-06"
score: 3/5 success criteria covered within implemented fixture
reviewed_sha: 71502ce37e110aef3af7e1549c39dbf35d8be19c
human_verification_pending: true
gaps:
  - gap_id: G05-01
    truth: "Node runtime performs bounded regional failover, probes, periodic health and remote renewal"
    status: failed
    reason: "Main calls single-address Run; ConnectRegion has no per-candidate deadline; probe/report/renewal scheduling is absent"
    artifacts:
      - path: cmd/vortex-node/main.go
        issue: "Single Hub endpoint only"
      - path: agent/control.go
        issue: "Regional helper is not wired into main"
    missing:
      - "Bounded regional control loop with health preference and stalled-peer failover tests"
      - "Probe, health/version and authorization-renewal scheduler with persistent bounds"
  - gap_id: G05-02
    truth: "Single node binary exposes operational commands and performs service-level safe upgrades"
    status: failed
    reason: "Installer exists as a library; no runtime invocation, systemd package or actual session drain/replacement demonstration"
    artifacts:
      - path: cmd/vortex-node/main.go
        issue: "No enroll/run/status/drain command surface or installer orchestration"
      - path: agent/upgrade/installer.go
        issue: "Callbacks not connected to running node lifecycle"
    missing:
      - "Service integration and signed upgrade/rollback process tests preserving durable financial state"
  - gap_id: G05-03
    truth: "Control and VLink credentials rotate and revoke consistently across restart and overlap"
    status: failed
    reason: "Control renewal is wired but runtime Engine identity is loaded independently"
    artifacts:
      - path: agent/credentials.go
        issue: "No verified Engine credential reload path"
      - path: agent/engine_runtime.go
        issue: "Runtime identity selected at construction"
    missing:
      - "Common validated identity provider and expired/revoked connection-admission regressions"
  - gap_id: G05-04
    truth: "Enrollment uses bounded connections, bytes and time with cancellation cleanup"
    status: resolved
    reason: "Resolved by f6a5891; repeated race, Docker/Actions 42140165239 and downloaded snapshot audit passed"
    artifacts:
      - path: hub/enrollment.go
        issue: "Resource bounds verified with real TLS tests and retained paired evidence"
    missing: []
  - gap_id: G05-05
    truth: "Runtime distinguishes recovering/caught-up and enforces complete share/policy transitions"
    status: failed
    reason: "Atomic S/R slice works but operational recovery mode/share-reference and policy scheduling are incomplete"
    artifacts:
      - path: agent/store/handoff.go
        issue: "Handoff returns online mode without separate caught-up runtime state"
    missing:
      - "Recovering-mode capability enforcement and caught-up checkpoint transition"
      - "Durable share-reference/CAS and scheduled policy-boundary integration; backend authority remains Phase 6"

---

# Phase 5 goal verification

Verdict: **gaps_found**. Five executed plan slices do not establish the entire phase goal. The owner-approved authorization 1.1.0 proposal remains recorded exactly as approved; signer epoch stays independent of path generation.

| ROADMAP success criterion | Verdict | Evidence / remaining work |
|---|---|---|
| 1. Single node, identity separation, control renewal, drain and rollback | PARTIAL | Actual destination runtime, enrollment and renewal work; service upgrade/drain, regional command integration and shared VLink identity lifecycle remain G05-01/02/03. |
| 2. FULL-before-ACK, SQLite parameters and fault survival | VERIFIED-SLICE | Real disk-full/SIGKILL/replay tests and snapshot audits; finite tests do not certify physical power loss. |
| 3. Restricted autonomy and fenced recovery | PARTIAL | Original deadline/scope and one-successor transaction covered; operational share-chain and recovering state integration remain G05-05. |
| 4. Atomic S/R and prefix/tail replay | VERIFIED-SLICE | Five independently audited snapshots; original R transferred once, unknown debt retained, hash-bound checkpoint. Backend is a protocol fixture per D-19. |
| 5. Unknown activation and proof-before-release | VERIFIED-SLICE | Actual fresh-boot Engine reactivation; concurrent Commit/Abort, late results and process/execution proof tests. |

All 34 IDs are accounted for in `05-REQUIREMENT-AUDIT.md`. Security register: no blocking high threats after 71502ce, the medium bootstrap resource-exhaustion threat is now fixed and paired-tested. Nyquist coverage remains partial because missing runtime behaviors cannot be satisfied by library-only tests.

## Evidence

- Prior plan acceptance pair: 79a1c0b, Actions 42132354600, `05-04-EVIDENCE.md`.
- Safety-fix pair: 71502ce, Docker gate and Actions 42138697667 both passed on the same clean source SHA. Downloaded snapshots independently re-audited in Linux: five snapshots and ten required fault cases per host; competing Commit/Abort passed on both. Source-bound pair.json and SHA256.json retain linkage and hashes.
- R1 red/green reproduction: `.planning/debug/phase5-unverified-control-peer.md`.
- Full current test/contract run is `make phase5-check`; performance and physical power-loss claims are excluded.

## Gaps and routing

G05-01..05 source-grounded groups are detailed in `05-REQUIREMENT-AUDIT.md` and review disposition. Keep Phase 5 verifying; run end-of-phase UAT and then `$gsd-plan-phase 5 --gaps`. Do not advance Phase 6 or mark Phase 5 complete from these passing slices alone. Phase 4 remains separate and is not reopened by this audit.

Security follow-up: f6a5891 passes Docker/Actions run 42140165239; both downloaded/local evidence sets independently re-audited. See `artifacts/phase5/check/f6a58916336e0f754a67252ad54c1091883e8573/pair.json`. G05-04 is resolved; remaining runtime integration gaps keep the phase verdict unchanged.
