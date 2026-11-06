# Phase 5 requirement audit — 2026-11-06

Source reviewed: deb8b08 (implementation through 05-14 consolidated gate); same-source Docker/Actions and renewed UAT passed. Current ROADMAP/CONTEXT decisions govern. V1.11 IDs retain useful semantics; old numeric defaults are not newly imposed acceptance gates. COVERED-SLICE means current bounded implementation/test evidence, not product-wide requirement completion. No global requirements-completed flags are added. TRANSFER-PARTIAL explicitly preserves Phase 6 work, rather than marking the requirement passed.

| Requirement | Status | Current source / test evidence | Limit / gap |
|---|---|---|---|
| AGT-001 | COVERED-SLICE | agent/registration.go; hub/enrollment_security_test.go; test/phase5/node_commands_test.go | Local CSR, one-use bootstrap fixture and enroll/run command covered; production token/CA lifecycle belongs Phase6. |
| AGT-002 | COVERED-SLICE | agent/regional_control.go; test/phase5/regional_process_test.go | 05-07/08 actual bounded region runtime verified; production backend integration remains Phase 6. |
| AGT-003 | COVERED-SLICE | agent/store/store.go; agent/store/store_test.go | Independent WAL/FULL store and durable config/grant/usage records; finite crash tests. |
| AGT-004 | COVERED-SLICE | agent/reconcile.go; agent/recovery_test.go | Durable per-object prepare/commit/abort and execution-proof query. |
| AGT-005 | COVERED-SLICE | agent/renewal.go; test/phase5/scheduler_process_test.go; test/phase5/share_policy_process_test.go | Durable scoped renewal, policy cutover and separate future funding; production allocation Phase6. |
| AGT-006 | COVERED-SLICE | agent/usage.go; agent/store/usage_retention.go; independent ledger audits | FULL/unknown/full-disk and conservative settled transport cleanup verified; original journal retained; production backend settlement remains Phase 6. |
| AGT-007 | COVERED-SLICE | agent/scheduler.go; agent/scheduler_test.go | Bounded installed-target probes/current-session periodic reports verified in 05-08. |
| AGT-008 | COVERED-SLICE | agent/upgrade/service.go; test/phase5/upgrade_process_test.go | Real signed replacement/failed-candidate rollback, termination witness and no financial rollback; systemd units verified, daemon boot not claimed. |
| AGT-012 | COVERED-SLICE | agent/identity_runtime.go; agent/identity_control_test.go; test/phase5/identity_process_test.go | 05-09 shared control/VLink live rotation, durable revocation and expiry/overlap/failure tests passed on a9aea9d Docker/Actions42181891508; production CA authority remains Phase 6. |
| AGT-009 | COVERED-SLICE | agent/runtime_restart_test.go | Fresh boot fences old grants as settlement-only and preserves debt. |
| AGT-010 | COVERED-SLICE | agent/runtime_restart_test.go; agent/runtime_activation.go | Actual configured destination re-Prepare/current authority/Commit; hot/source/relay integration not claimed. |
| AGT-011 | COVERED-SLICE | agent/authorization_gate.go; agent/runtime_authority_test.go | Installed authorization bounds fail closed; no broader runtime topology certification. |
| HUB-001 | COVERED-SLICE | hub/testhub.go; agent/control_security_test.go | Verified mTLS peer, SAN identity and fixture revocation; production CA ownership Phase 6. |
| HUB-002 | COVERED-SLICE | hub/issuer.go; agent/store/share_refs.go; test/phase5/share_policy_process_test.go | Only separately provisioned delegation, complete current chain and original bounds; dynamic backend rule compiler Phase6. |
| HUB-003 | COVERED-SLICE | hub/usage.go; agent/usage_handoff_test.go | Online backend ACK / offline local FULL commit paths covered with protocol backend. |
| HUB-004 | COVERED-SLICE | hub/status.go; agent/scheduler_test.go | Bounded latest health/version/probe cache and runtime reports verified in 05-08. |
| HUB-005 | TRANSFER-PARTIAL | hub/usage.go; hub/recovery.go | Backend protocol fixture only; production regional backend transport Phase 6 per D-19. |
| HUB-006 | TRANSFER-PARTIAL | hub/issuer.go | Local delegation conservation covered; authoritative regional allocation strategy Phase 6. |
| HUB-007 | COVERED-SLICE | agent/store/autonomy_test.go; agent/store/issuer_test.go | Persisted heartbeat-origin grace and scope restrictions covered. |
| HUB-008 | COVERED-SLICE | agent/store/autonomy_test.go | Fixed original hard deadline survives restart; no deadline extension. |
| HUB-009 | COVERED-SLICE | agent/store/issuer_test.go | Reject unknown time/expanded scope/expired grant; old rights remain bounded. |
| HUB-010 | COVERED-SLICE | agent/store/handoff_test.go | Revoke-first recovery transaction; rollback on invalid revocation. |
| HUB-011 | COVERED-SLICE | agent/store/share_refs.go; agent/store/share_occupancy_test.go; test/phase5/share_policy_process_test.go | Complete chain/issuer/revision/CAS; unknown configuration and session occupancy retained until proof-backed release. |
| HUB-012 | COVERED-SLICE | agent/store/store.go; hub/recovery_test.go | Independent WAL, sequence and replay; unresolved records retained, destructive GC not enabled. |
| HUB-013 | COVERED-SLICE | agent/store/recovery_mode.go; test/phase5/recovering_process_test.go | Persistent finite recovering target, contiguous hash-bound catch-up and independent live tail; no reconnect promotion. |
| HUB-014 | COVERED-SLICE | agent/regional_control.go; test/phase5/regional_process_test.go | Actual online-preferred regional failover verified in 05-07/08; production backend remains Phase 6. |
| HUB-015 | COVERED-SLICE | agent/store/epoch_handoff_test.go; agent/recovery_test.go | No transfer/release without ownership and execution proof; unknown debt persists. |
| HUB-016 | COVERED-SLICE | agent/store/issuer.go; agent/store/share_refs.go; agent/store/share_occupancy_test.go | Finite source reservation, parent capacity and retained holds covered; production allocator remains Phase6. |
| HUB-017 | COVERED-SLICE | agent/epoch_handoff_test.go; agent/store/handoff_test.go | Old signer cannot revive; unique successor R cap enforced. |
| HUB-018 | COVERED-SLICE | agent/store/policy_transitions.go; test/phase5/share_policy_process_test.go | Actual TCP/UDP old-session pinning/new-snapshot cutover, guard, revocation and Hub restart; production policy authority remains Phase6. |
| SUP-AGENT-001 | COVERED-SLICE | cmd/vortex-node/commands.go; deploy/systemd/vortex-node.service; test/phase5/node_commands_test.go | enroll/run/status/drain/upgrade and container supervision delivered; actual systemd daemon acceptance separate from verified units/real process tracer. |
| SUP-B1-001 | COVERED-SLICE | agent/authority_clock.go; agent/authority_clock_test.go | Boot-local conservative trusted interval; expired/contradictory evidence fails closed. |
| SUP-B3-001 | COVERED-SLICE | agent/store/share_refs.go; agent/store/share_occupancy.go; test/phase5/share_policy_process_test.go | Durable complete lineage, current revision admission and config/session occupancy; backend source provisioning Phase6. |
| SUP-B4-001 | COVERED-SLICE | agent/store/handoff_test.go; hub/recovery_test.go | Atomic original R transfer and ongoing idempotent tail covered by fixture. |

## Gap disposition after05-13

- G05-01: runtime regional failover/probe/report/renewal resolved by05-07/08; full regressions retained.
- G05-02: commands, deployment units/container, real drain/replacement/rollback resolved by05-10/11 within static-destination runtime scope.
- G05-03: shared control/VLink identity and revocation resolved by05-09; production CA remains Phase6.
- G05-04: bounded enrollment resolved byf6a5891 and protected by real network regressions.
- G05-05: finite recovering barrier resolved by05-12; complete share holds/policy boundary resolved by05-13.

All 34 IDs are mapped above. HUB-005/006 remain TRANSFER-PARTIAL for production backend transport/allocation, explicitly owned by Phase 6 under D-19; current Agent/Hub adapters have finite integration evidence. COVERED-SLICE is the current Phase 5 scope, never whole-product certification. The 05-14 clean same-SHA Linux pair, evidence integrity negatives, review/security/Nyquist and renewed owner UAT passed on deb8b08. Historical audit dispositions remain in Git history rather than presented as current missing implementation.
