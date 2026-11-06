# Phase 5 requirement audit — 2026-11-06

Source reviewed: 71502ce. Current ROADMAP/CONTEXT decisions govern. V1.11 IDs retain useful semantics; old numeric defaults are not newly imposed acceptance gates. COVERED-SLICE means current bounded implementation/test evidence, not product-wide requirement completion. No requirements-completed flags are added. TRANSFER-PARTIAL explicitly preserves Phase 6 work, rather than marking the requirement passed.

| Requirement | Status | Current source / test evidence | Limit / gap |
|---|---|---|---|
| AGT-001 | PARTIAL | agent/registration.go; agent/identity/identity_test.go | Local CSR/one-use fixture covered; durable production token lifecycle belongs Phase 6. |
| AGT-002 | COVERED-SLICE | agent/regional_control.go; test/phase5/regional_process_test.go | 05-07/08 actual bounded region runtime verified; production backend integration remains Phase 6. |
| AGT-003 | COVERED-SLICE | agent/store/store.go; agent/store/store_test.go | Independent WAL/FULL store and durable config/grant/usage records; finite crash tests. |
| AGT-004 | COVERED-SLICE | agent/reconcile.go; agent/recovery_test.go | Durable per-object prepare/commit/abort and execution-proof query. |
| AGT-005 | COVERED-SLICE | agent/renewal.go; test/phase5/scheduler_process_test.go | Finite scoped remote renewal and seven process faults verified; broader share/policy transitions remain G05-05. |
| AGT-006 | COVERED-SLICE | agent/usage.go; agent/store/usage_retention.go; independent ledger audits | FULL/unknown/full-disk and conservative settled transport cleanup verified; original journal retained; production backend settlement remains Phase 6. |
| AGT-007 | COVERED-SLICE | agent/scheduler.go; agent/scheduler_test.go | Bounded installed-target probes/current-session periodic reports verified in 05-08. |
| AGT-008 | PARTIAL | agent/upgrade/installer.go; agent/upgrade/installer_test.go | Real file installer covered; service-level drain/restart/rollback integration absent. G05-02. |
| AGT-012 | COVERED-SLICE | agent/identity_runtime.go; agent/identity_control_test.go; test/phase5/identity_process_test.go | 05-09 shared control/VLink live rotation, durable revocation and expiry/overlap/failure tests passed on a9aea9d Docker/Actions42181891508; production CA authority remains Phase 6. |
| AGT-009 | COVERED-SLICE | agent/runtime_restart_test.go | Fresh boot fences old grants as settlement-only and preserves debt. |
| AGT-010 | COVERED-SLICE | agent/runtime_restart_test.go; agent/runtime_activation.go | Actual configured destination re-Prepare/current authority/Commit; hot/source/relay integration not claimed. |
| AGT-011 | COVERED-SLICE | agent/authorization_gate.go; agent/runtime_authority_test.go | Installed authorization bounds fail closed; no broader runtime topology certification. |
| HUB-001 | COVERED-SLICE | hub/testhub.go; agent/control_security_test.go | Verified mTLS peer, SAN identity and fixture revocation; production CA ownership Phase 6. |
| HUB-002 | PARTIAL | hub/issuer.go; agent/store/issuer_test.go | Installed delegated scope only; complete dynamic rule/share integration G05-05. |
| HUB-003 | COVERED-SLICE | hub/usage.go; agent/usage_handoff_test.go | Online backend ACK / offline local FULL commit paths covered with protocol backend. |
| HUB-004 | COVERED-SLICE | hub/status.go; agent/scheduler_test.go | Bounded latest health/version/probe cache and runtime reports verified in 05-08. |
| HUB-005 | TRANSFER-PARTIAL | hub/usage.go; hub/recovery.go | Backend protocol fixture only; production regional backend transport Phase 6 per D-19. |
| HUB-006 | TRANSFER-PARTIAL | hub/issuer.go | Local delegation conservation covered; authoritative regional allocation strategy Phase 6. |
| HUB-007 | COVERED-SLICE | agent/store/autonomy_test.go; agent/store/issuer_test.go | Persisted heartbeat-origin grace and scope restrictions covered. |
| HUB-008 | COVERED-SLICE | agent/store/autonomy_test.go | Fixed original hard deadline survives restart; no deadline extension. |
| HUB-009 | COVERED-SLICE | agent/store/issuer_test.go | Reject unknown time/expanded scope/expired grant; old rights remain bounded. |
| HUB-010 | COVERED-SLICE | agent/store/handoff_test.go | Revoke-first recovery transaction; rollback on invalid revocation. |
| HUB-011 | PARTIAL | agent/store/issuer.go; agent/store/handoff.go | Source isolation covered; full allowed-issuer/share-chain references incomplete. G05-05. |
| HUB-012 | COVERED-SLICE | agent/store/store.go; hub/recovery_test.go | Independent WAL, sequence and replay; unresolved records retained, destructive GC not enabled. |
| HUB-013 | PARTIAL | agent/store/handoff.go; hub/recovery_test.go | Atomic S/R and tail replay covered; distinct recovering/caught-up runtime state incomplete. G05-05. |
| HUB-014 | COVERED-SLICE | agent/regional_control.go; test/phase5/regional_process_test.go | Actual online-preferred regional failover verified in 05-07/08; production backend remains Phase 6. |
| HUB-015 | COVERED-SLICE | agent/store/epoch_handoff_test.go; agent/recovery_test.go | No transfer/release without ownership and execution proof; unknown debt persists. |
| HUB-016 | PARTIAL | agent/store/issuer_test.go | Source reservation covered; full share-chain integration G05-05, backend allocator Phase 6. |
| HUB-017 | COVERED-SLICE | agent/epoch_handoff_test.go; agent/store/handoff_test.go | Old signer cannot revive; unique successor R cap enforced. |
| HUB-018 | PARTIAL | agent/runtime_authority.go | Immutable installed policy bounds covered; scheduled policy transition protocol not integrated. G05-05 / Phase 6 authority. |
| SUP-AGENT-001 | MISSING | cmd/vortex-node/main.go | Single binary exists; enroll/run/status/drain command surface and systemd package missing. G05-02. |
| SUP-B1-001 | COVERED-SLICE | agent/authority_clock.go; agent/authority_clock_test.go | Boot-local conservative trusted interval; expired/contradictory evidence fails closed. |
| SUP-B3-001 | PARTIAL | agent/store/authorization.go; agent/runtime_authority.go | Fencing and occupancy covered; full durable share-reference/CAS chain integration missing. G05-05. |
| SUP-B4-001 | COVERED-SLICE | agent/store/handoff_test.go; hub/recovery_test.go | Atomic original R transfer and ongoing idempotent tail covered by fixture. |

## Remaining gap groups

- G05-01: **resolved within planned adapter scope** by 05-07/08; final0c104a6 passed Docker/Actions42175493658 and independent ledger audits. No production backend authority introduced; shared IDs remain pending for later plans.
- G05-02: node command surface, systemd/container packaging and actual Engine drain/upgrade/restart/rollback with durable-state preservation.
- G05-03: resolved in 05-09 on a9aea9d with same-SHA Docker/Actions42181891508. Actual static destination runtime rotation and new OPEN revocation/expiry verified; production CA authority remains Phase 6.
- G05-04: **resolved** by f6a5891 and same-SHA Docker/Actions 42140165239: bounded enrollment concurrency, TLS/read time, request bytes and cancellation; real slow/oversized/overload/cancel tests.
- G05-05: runtime recovering/caught-up state, full share-reference/fencing and scheduled policy-boundary integration; keep production backend allocator in Phase 6.

Next workflow: `$gsd-plan-phase 5 --gaps` researches and creates executable closure plans from these source-grounded gaps. This audit is not itself an approved replacement implementation plan.

Incremental evidence update:05-08 completed on0c104a6; original source71502ce audit remains historical for unchanged rows. Full phase audit is still required after05-09..14.
