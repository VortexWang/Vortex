---
phase: 06-m4-m4-backend
plan: 03
status: resolved-plan-complete
source_commit: f285137bdebc2c541327922fbdc4ed41516cfe69
proposal_sha256: b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4
---

# 06-03 contract approval and reverification checkpoint

Implementation is present in commits da7b5fcc through f285137b. This is an
incomplete plan checkpoint, not a completion SUMMARY. The owner subsequently explicitly approved CR-005 with “批准 CR-005”. Approval
is recorded separately in docs/changes/CR-005-APPROVAL.json; it was never inferred
from the instruction to continue Phase 6.

## Reviewable decision

Owner approved the exact `docs/changes/CR-005-rule-two-phase.json` review unit, SHA-256
`b02a73a9d3d4d8506cfd79904efa6a5ebc430cd4b06d729a37b518e19d582bb4`:

| Contract | Proposed version | Compatibility |
|---|---|---|
| vortex.database | 1.0.0 -> 2.0.0 | Breaking relative to the historical conceptual schema; existing Phase 6 00004 databases upgrade using additive 00005. No historical 1.0 deployment migration is supplied. |
| vortex.public-api | 1.0.0 -> 2.0.0 | Breaking Restriction/ApiError/strict object changes; route namespace stays /api/v1. HTTP implementation remains 06-07. |
| vortex.control.v1 | 1.3.0 -> 1.4.0 | Additive negotiated RuleCommand/RuleReceipt; old automatic configuration updates retain their meaning. |
| vortex.runtime-config | 1.1.0 -> 1.2.0 | Separate immutable rule artifact branch; complete legacy schema retained. |

The proposal pins all normative/generated artifact hashes and explains migration
limits. `docs/governance/contract-review.yaml:66` explicitly requires owner
approval of normative changes. Prior approval-history entries remain unchanged; four new approvals now bind the accepted bytes.

## Historical candidate evidence (before approval)

Evidence directory:
`artifacts/phase6/06-03/f285137bdebc2c541327922fbdc4ed41516cfe69/docker/`.

- Clean exact source clone, Go 1.27.3 in Linux arm64 Docker; source identity and
  before/after status are retained and checksum-bound by result.json.
- Full backend normal/race suite: 204 PASS lines, zero skips/failures/race warnings.
  Real PostgreSQL migrations reach 00005; deterministic sqlc output, dependency
  and license checks and OpenAPI lint/copy checks pass.
- Contract/Agent/Engine normal/race suites: 1,282 PASS lines, zero
  skips/failures/race warnings. Includes real sockets, TLS multihop forward/reverse,
  unknown/query/restart, explicit release, and legacy schema regression tests.
- Linux amd64/arm64 compile metadata is retained. This is cross-compilation,
  not native amd64 execution or performance qualification.
- Real Cloudflare create/read/update/delete passes with TTL=60, DNS-only,
  encrypted storage and exact-name cleanup. Policy provenance remains a reviewed
  creation-policy attestation, not independent live-policy introspection.
- Candidate generation/wire/schema checks pass. All four frozen checks remain
  failed pending owner approval. result.json reports result=0 for executed checks
  and acceptance_complete=false. The overall producer acceptance gate is not passed.
- Re-ran evidence_manifest.sh for through-plan 06-03 and host docker after copying
  evidence into the repository: checksum and required named normal/race tests pass;
  producer-only success explicitly leaves pairing pending.
- No Actions result is claimed for f285137b. Final same-SHA Docker/Actions pairing
  and all remaining phase gates remain 06-11 obligations.

## Resume after decision

If approved, record the actual owner decision and exact hashes, freeze the four
candidate contract entries, adapt the candidate checker to signed approval,
and rerun frozen/generated/compatibility and exact-source producer checks.
Then audit all 06-03 acceptance requirements and create its SUMMARY only if they
pass. Reconcile WINDOWS 6/7 with evidence and update state/roadmap/requirements
after the SUMMARY commit. Continue dependency order 06-04 -> 06-05 -> 06-06 ->
06-07 -> 06-11. Missing billing/source producers remain later-plan work;
no invented snapshots or funding may be used to close acceptance.

If rejected, revise the implementation and regenerate the exact proposal review
unit. Never sign an approval on behalf of the owner or mark 06-03 complete while
the freeze remains pending. Preserve unrelated caller Phase 4/5 changes.

## Approval and post-approval work

- Actual owner response: “批准 CR-005” (repeated in-session). No extra approval
  is required for this exact review unit. The immutable proposal JSON and all
  normative candidate bytes remain unchanged; catalog statuses become frozen
  with the before/after hashes recorded in CR-005-APPROVAL.json.
- Four selected frozen checks, exact approved hash checks, two clean protobuf
  generations and legacy wire/schema compatibility pass after approval. The
  all-contract check still reports the pre-existing VLink hash mismatch; this
  approval does not authorize rewriting that separate historical record.
- Post-checkpoint audit found that a late old Prepare/query observation could
  recreate old Commit tasks. RED c21acb0e proved all three cases on real PostgreSQL.
  Fix 4e902c49 serializes on the rule head before deployment/task locks, refreshes
  observed state, and queues only abort/query for obsolete prepared/unknown work.
  Fresh-database normal/race regression and two clean sqlc generations pass.
- Exact committed-source Docker producer must be rerun with these changes.
  Acceptance and final same-SHA Actions pairing remain unclaimed.

## Resolved close-out

Owner-approved source `fc63d57406f9f0e3eebe99ad276f95b004add710` passed the complete clean local Docker producer (exit0), all four selected frozen checks, generated/wire/schema compatibility, backend and component normal/race, dual-architecture compilation and real Cloudflare CRUD. Evidence is committed at `6b74cc5b`; canonical completion is `06-03-SUMMARY.md` (`a9f2e190`). This checkpoint is resolved; next plan is 06-04. Final Actions pairing remains 06-11.
