# 06-02 node and port authority

The executable authority is migration 00003 plus scoped sqlc repositories and
services. Migrations 00001/00002 are unchanged. `platform_grants` is a view of
the existing resource_grants authority, not a second ownership store.

| Requirements | Executable evidence |
| --- | --- |
| NOD-001, NOD-005, NOD-006, NOD-007, NOD-009, NOD-014 | nodes/groups CHECKs and member_authority trigger; TestNodeGroupOwnershipAndStableMembers |
| NOD-002, NOD-003, NOD-004 | TestNodeLifecycleDrainDeleteAndAuditRollback; Impact preview; default 300-second desired drain; dependency and real port-occupation checks; soft deletion preserves history |
| NOD-008 | TestNodeGrantValidityHistoryAndAudit; grants retain platform ownership, validity, version CAS and immutable grant_history |
| NOD-010, NOD-011, NOD-012 | CheckSegment and TestNodeSegmentPermitIndependentOfGrants; compiler consumption is 06-03, session enforcement remains the Engine boundary |
| NOD-013 | PortRepository.Serving and TestPortAtomicFixedMembersAndServing; zero confirmed members means unavailable |
| PRT-001, PRT-002 | node link/control/metrics ports and service port range; guard_physical_port trigger; TestPortConcurrentWildcardConflict; TestPortPlatformCrossTenantPhysicalConflict |
| PRT-003, PRT-004, PRT-005, PRT-006 | TestPortAtomicFixedMembersAndServing; TestPortNoCommonPortAndMemberChanges; full stable set, common TCP/UDP port, safe common alternative on conflict, atomic prepare and all-member confirmation |
| PRT-007 | SetMembers marks every affected allocation needs_revalidation and appends a group-wide rule revalidation task; TestPortNoCommonPortAndMemberChanges |
| PRT-008, PRT-016 | TestPortRestrictionClockAndProofBackedRelease; TestPortRetentionClearReapplyAndManualHold; TestPortRestrictionAuditRollbackSameTransaction; first-effective clock survives repeats/cause changes; final clear resets clock; manual stop holds indefinitely |
| PRT-009 | TestPortProofAuthenticationAndGenerationFence; TestReleaseOldBootAndDisconnectRetainOccupation; unknown, DNS removal and stream loss preserve occupation |
| PRT-010 | released rule creates a fresh allocation and returns EntryAddressMayChange; TestPortRestrictionClockAndProofBackedRelease |

All read and mutation services accept TenantScope; sensitive reads, including
platform cross-tenant reads, commit an audit before returning results. Node,
grant, permit, restriction and port mutations use the shared AuditWriter.
Injected audit failures test rollback of business rows, immutable proof/history,
operations, tasks and Outbox. The full suite includes original 00001-to-current
upgrade verification with pre-existing authority rows.

Proof success tests use the server state from a real TLS 1.3 mutual handshake.
PortService pins the dedicated node CA, independently verifies the certificate,
and requires the node identity, current boot/generation, reservation task,
task kind, persisted lease token and target revision. A caller with only a
tenant/platform service capability cannot mint a receipt. Hub liveness and
foreign trust roots cannot prove release. All nodes must confirm configuration
and port reservation before commit; resuming a retained allocation requires a
new prepare task and new confirmations. Unknown results do not free sockets.

Domain changes use a distinct desired-state identity and a typed task_kind;
the existing RuntimeConfig delivery worker leases only control tasks. The
resource_change/port_prepare/port_release task consumers and public HTTP
adapters are later orchestration work (06-03/06-07). This plan tests the durable
SQL/task/proof boundary and real TLS authentication; it does not claim that a
production Agent has already executed a new port task through that future
consumer. Existing backend/Agent/blocked-Hub process tests still run unchanged.

Serving emits only currently authorized, online, incarnation-matching, fully
confirmed addresses. DNS is a consumer of this query, never an authority for
port release. Real Cloudflare CRUD, TTL=60, DNS-only, minimal domain scopes and
fail-closed credential behavior belong to 06-10 and have not been claimed here.
encrypted_credentials has scoped uniqueness, ciphertext, 12-byte nonce,
key_version and scope, with no plaintext field. Its consumer is 06-10.

The producer supports through-plan 06-02. Local Docker and actual Actions
results are independent evidence; only the paired manifest accepts both for
one SHA. A Docker-only run leaves Actions/paired acceptance pending. Final
full-phase pairing remains required in 06-11. Phase 4 performance/D-04,
source conservation/Valkey-debt work, and Phase 7/8 acceptance are unchanged.

## Current cross-domain integration

The paragraphs above describe the original 06-02 delivery boundary. Plans06-03
and06-07 now provide production rule/port consumers and REST operations. Final
source65a53358 passed Docker and native Actions42572674719; final acceptance
is recorded separately in06-VERIFICATION.md.

NOD-012 is traced through NodeService.CheckSegment, compiler validation of the
directed permitted path and immutable per-node artifact, then Engine's committed
rule lookup on every OPEN. The Engine checks upstream identity/incarnation, hop,
revision/path generation, allowed protocol and loop state before opening the
session. Evidence: TestNodeSegmentPermitIndependentOfGrants,
TestCompileGrantPermitAndDirection, TestApplyMultihopForwardAndReverseRealTLS,
and TestOpenPolicyStableRejectionClassesAndAuditCorrelation. The immutable
artifact represents the compile-time permit; Engine does not query PostgreSQL.

PRT-004 combines multi-member service atomicity with actual Agent/Engine socket
execution: TestPortAtomicFixedMembersAndServing,
TestRulePortActualAgentSocketsUnknownAndQuery, and
TestPortIntentAbortDoesNotReleaseWithoutAllNodeProofs. The real socket fixture
uses one member on one host; fixed-member tests independently verify all-member
confirmation and rollback. Unknown execution retains occupation until proof.

当前最终验收：实现提交`65a5335894732b7d632e870acc3d8283be224cd3`，Docker及Actions42572674719原生amd64/arm64完整通过（含真实Cloudflare）。原始证据`artifacts/phase6/06-11/final-65a53358`；阶段完整结论与保留范围见06-VERIFICATION.md。以上较早证据仍按当时范围解释。
