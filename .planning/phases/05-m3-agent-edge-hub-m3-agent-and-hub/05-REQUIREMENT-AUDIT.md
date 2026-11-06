# Phase 5 requirement audit — 2026-11-06

Source reviewed: 71502ce. Current ROADMAP/CONTEXT decisions govern. V1.11 IDs retain useful semantics; old numeric defaults are not newly imposed acceptance gates. COVERED-SLICE means current bounded implementation/test evidence, not product-wide requirement completion. No requirements-completed flags are added. TRANSFER-PARTIAL explicitly preserves Phase 6 work, rather than marking the requirement passed.

| Requirement | Status | Current source / test evidence | Limit / gap |
|---|---|---|---|
| AGT-001 | PARTIAL | agent/registration.go; agent/identity/identity_test.go | Local CSR/one-use fixture covered; durable production token lifecycle belongs Phase 6. |
| AGT-002 | PARTIAL | agent/control.go:Run/ConnectRegion; cmd/vortex-node/main.go | Single-address reconnect covered; regional deadline/failover not wired. G05-01. |
| AGT-003 | COVERED-SLICE | agent/store/store.go; agent/store/store_test.go | Independent WAL/FULL store and durable config/grant/usage records; finite crash tests. |
| AGT-004 | COVERED-SLICE | agent/reconcile.go; agent/recovery_test.go | Durable per-object prepare/commit/abort and execution-proof query. |
| AGT-005 | PARTIAL | agent/runtime_authority.go; agent/store/runtime_renewal_test.go | Bounded installed authorization works; timed remote renewal/request scheduler incomplete. G05-01. |
| AGT-006 | PARTIAL | agent/usage.go; agent/unknown_usage_test.go | Durability/unknown/full-disk covered; complete operational reporting cadence and retention budget not established. G05-01. |
| AGT-007 | MISSING | cmd/vortex-node/main.go | No Agent probe scheduler + report integration. G05-01. |
| AGT-008 | PARTIAL | agent/upgrade/installer.go; agent/upgrade/installer_test.go | Real file installer covered; service-level drain/restart/rollback integration absent. G05-02. |
| AGT-012 | PARTIAL | agent/credentials.go; agent/engine_runtime.go | Control renewal wired; common Engine VLink rotation/revocation unverified. G05-03. |
| AGT-009 | COVERED-SLICE | agent/runtime_restart_test.go | Fresh boot fences old grants as settlement-only and preserves debt. |
| AGT-010 | COVERED-SLICE | agent/runtime_restart_test.go; agent/runtime_activation.go | Actual configured destination re-Prepare/current authority/Commit; hot/source/relay integration not claimed. |
| AGT-011 | COVERED-SLICE | agent/authorization_gate.go; agent/runtime_authority_test.go | Installed authorization bounds fail closed; no broader runtime topology certification. |
| HUB-001 | COVERED-SLICE | hub/testhub.go; agent/control_security_test.go | Verified mTLS peer, SAN identity and fixture revocation; production CA ownership Phase 6. |
| HUB-002 | PARTIAL | hub/issuer.go; agent/store/issuer_test.go | Installed delegated scope only; complete dynamic rule/share integration G05-05. |
| HUB-003 | COVERED-SLICE | hub/usage.go; agent/usage_handoff_test.go | Online backend ACK / offline local FULL commit paths covered with protocol backend. |
| HUB-004 | MISSING | hub/testhub.go; agent/control.go | Complete health/version/probe cache and runtime reporting absent. G05-01. |
| HUB-005 | TRANSFER-PARTIAL | hub/usage.go; hub/recovery.go | Backend protocol fixture only; production regional backend transport Phase 6 per D-19. |
| HUB-006 | TRANSFER-PARTIAL | hub/issuer.go | Local delegation conservation covered; authoritative regional allocation strategy Phase 6. |
| HUB-007 | COVERED-SLICE | agent/store/autonomy_test.go; agent/store/issuer_test.go | Persisted heartbeat-origin grace and scope restrictions covered. |
| HUB-008 | COVERED-SLICE | agent/store/autonomy_test.go | Fixed original hard deadline survives restart; no deadline extension. |
| HUB-009 | COVERED-SLICE | agent/store/issuer_test.go | Reject unknown time/expanded scope/expired grant; old rights remain bounded. |
| HUB-010 | COVERED-SLICE | agent/store/handoff_test.go | Revoke-first recovery transaction; rollback on invalid revocation. |
| HUB-011 | PARTIAL | agent/store/issuer.go; agent/store/handoff.go | Source isolation covered; full allowed-issuer/share-chain references incomplete. G05-05. |
| HUB-012 | COVERED-SLICE | agent/store/store.go; hub/recovery_test.go | Independent WAL, sequence and replay; unresolved records retained, destructive GC not enabled. |
| HUB-013 | PARTIAL | agent/store/handoff.go; hub/recovery_test.go | Atomic S/R and tail replay covered; distinct recovering/caught-up runtime state incomplete. G05-05. |
| HUB-014 | PARTIAL | agent/control.go:ConnectRegion | No online-preferred runtime failover/reporting. G05-01. |
| HUB-015 | COVERED-SLICE | agent/store/epoch_handoff_test.go; agent/recovery_test.go | No transfer/release without ownership and execution proof; unknown debt persists. |
| HUB-016 | PARTIAL | agent/store/issuer_test.go | Source reservation covered; full share-chain integration G05-05, backend allocator Phase 6. |
| HUB-017 | COVERED-SLICE | agent/epoch_handoff_test.go; agent/store/handoff_test.go | Old signer cannot revive; unique successor R cap enforced. |
| HUB-018 | PARTIAL | agent/runtime_authority.go | Immutable installed policy bounds covered; scheduled policy transition protocol not integrated. G05-05 / Phase 6 authority. |
| SUP-AGENT-001 | MISSING | cmd/vortex-node/main.go | Single binary exists; enroll/run/status/drain command surface and systemd package missing. G05-02. |
| SUP-B1-001 | COVERED-SLICE | agent/authority_clock.go; agent/authority_clock_test.go | Boot-local conservative trusted interval; expired/contradictory evidence fails closed. |
| SUP-B3-001 | PARTIAL | agent/store/authorization.go; agent/runtime_authority.go | Fencing and occupancy covered; full durable share-reference/CAS chain integration missing. G05-05. |
| SUP-B4-001 | COVERED-SLICE | agent/store/handoff_test.go; hub/recovery_test.go | Atomic original R transfer and ongoing idempotent tail covered by fixture. |

## Remaining gap groups

- G05-01: wire bounded regional reconnect/failover, probes/periodic health and remote authorization renewal into the actual node/Hub protocol fixture. No production backend authority is introduced here.
- G05-02: node command surface, systemd/container packaging and actual Engine drain/upgrade/restart/rollback with durable-state preservation.
- G05-03: common control/VLink credential rotation and revocation, with expiry/restart/overlap admission tests.
- G05-04: bound enrollment concurrency, TLS/read time, request bytes and cancellation; negative slow/oversized-request tests.
- G05-05: runtime recovering/caught-up state, full share-reference/fencing and scheduled policy-boundary integration; keep production backend allocator in Phase 6.

Next workflow: `$gsd-plan-phase 5 --gaps` researches and creates executable closure plans from these source-grounded gaps. This audit is not itself an approved replacement implementation plan.
