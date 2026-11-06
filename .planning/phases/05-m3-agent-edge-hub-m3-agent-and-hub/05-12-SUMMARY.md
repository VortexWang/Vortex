---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: '12'
subsystem: hub-recovery
requires:
  - phase: 05-11
    provides: Verified real service lifecycle and financial monotonicity
provides:
  - Durable finite recovering checkpoint captured atomically with S/R
  - Transactional proof-bound recovery completion with independent live tail
  - Real Hub/Node TCP recovery and restart tracer
requirements-completed: [HUB-007, HUB-008, HUB-010, HUB-012, HUB-013, HUB-014, HUB-015, HUB-017, SUP-B4-001]
requirements-status-note: Scoped coverage only; all IDs still have incomplete sibling plans.
tech-stack:
  added: []
  patterns: [finite-catchup-barrier, durable-before-ack, additive-conservative-migration]
coverage:
  - id: durable-recovery-barrier
    description: Atomic S/R captures a fixed barrier; reconnect and historical migration cannot infer online
    verification:
      - kind: integration
        ref: agent/store/recovery_mode_test.go#TestRecoveryBarrierSurvivesRestartAndTail
        status: pass
      - kind: integration
        ref: agent/store/recovery_mode_test.go#TestRecoveryMigrationConservativelyCapturesHistoricalHandoff
        status: pass
      - kind: integration
        ref: agent/store/recovery_mode_test.go#TestRecoveryAtomicFailureAndConcurrentCompletion
        status: pass
    human_judgment: false
  - id: live-tail-progress
    description: Bounded replay exceeds1000 records and confirms finite barrier despite later lost tail receipt
    verification:
      - kind: integration
        ref: hub/recovery_test.go#TestRecoveryBoundedReplayReachesCapturedBarrierWithLiveTail
        status: pass
    human_judgment: false
  - id: actual-process-recovery
    description: Two real Hubs, Node/Engine and mutual TLS FULL receipt backend preserve TCP, R and unknown debt across failures
    verification:
      - kind: e2e
        ref: test/phase5/recovering_process_test.go#TestRecoveringProcessCatchup
        status: pass
      - kind: other
        ref: artifacts/phase5/check/1c06bc91ad9e8cb72493b875684d8740c89c0ee9/pair.json
        status: pass
    human_judgment: false
completed: 2026-11-06
status: complete
---

# 05-12 Durable recovering checkpoint

S/R handoff now persists recovering and a finite ledger target in the same FULL
transaction. New delegation scope stays frozen. Completion requires current
successor ownership, trusted current heartbeat and original deadline, contiguous
confirmed prefix and no holes; later tail stays independently pending. Historical
handoffs without a witness migrate conservatively; reopening never recaptures a
completed target or duplicates R.

Completed tracer and negative/failure tasks on source1c06bc9. Five actual-process
scenarios cover lost/forged receipts and SIGKILL before handoff, after handoff and
after catch-up. Existing TCP, node boot/path generation, unique R transfer and
original unresolved512-byte liability survive. Backend is an explicit mTLS
protocol fixture with separate FULL ledger, never production authority.

Full clean Docker arm64 and Ubuntu amd64 Actions42198081076 passed the same SHA.
Evidence/checksums, named race tests and independent raw ledger audits are in
05-12-EVIDENCE.md. Native full root tests and focused vet/race also passed.

Deviation: complete the finite barrier even when a later post-barrier receipt
fails, avoiding starvation by ongoing tail. Such error remains returned and its
tail record stays unconfirmed. No new wire contract/dependency introduced.
SQL abort injection is a finite transaction failure model; existing physical
process kill/full-disk suites remain separate evidence. This closes only the
recovery portion of G05-05. Phase5 remains incomplete;05-13 and05-14 remain.
