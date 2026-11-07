# CR-005 — revision-scoped rule control and runtime artifact

Status: pending owner approval. Review the exact JSON manifest and its SHA-256 recorded in contracts/CHANGELOG.md. The existing approved contract-review.yaml is preserved; implementation and passing tests do not imply a separate owner signature.

06-03 requires real fixed-member Prepare/Commit/query and authenticated port-release execution. The legacy RuntimeConfigUpdate auto-apply path cannot encode that distinction safely. Control 1.4.0 adds separate RuleCommand/RuleReceipt oneof tags and opt-in initial Heartbeat capability; runtime 1.2.0 adds the immutable artifact branch while preserving the full old schema. Old nodes receive no new phased body. Hub lifecycle stays 1.3.0, and Engine remains process-local without a control client or database.

The exact manifest pins protobuf, normative lifecycle companions, runtime schema, generated Go, catalog and requirement mapping. Review focuses on all-member preparation, receipt identity/fences, conservative unknown holds, explicit socket-stop release, legacy compatibility and immutable input/snapshot bindings. Billing/source producers remain later plans; missing inputs stay pending and do not generate authority.

Executable artifacts are limited to 16 path groups and 256 members per group, matching the existing runtime. Desired rule normalization may accept a longer path, but compilation rejects it with RULE_INVALID and emits no artifact; excess members return RULE_MEMBER_SET_INVALID. Boundary tests validate actual compiler bytes through both the published schema projection and the real runtime decoder.

After approval, record that actual decision and the exact candidate hashes in the frozen review, change the two catalog statuses to frozen, update the candidate review checker to validate the signed approval instead of the historical-record equality, regenerate the proposal review unit if any normative bytes change, and rerun frozen/generated/compatibility plus exact-source producer checks. Never replace prior approvals with a fabricated signature or advance Phase 6 to 6/11 before this gate and 06-03 close-out pass.
