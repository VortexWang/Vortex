# CR-002: NFR-038 Historical Evidence Recovery

Status: draft; pending user review and formal approval
Source baseline: V1.11
Proposed target baseline: V1.12
Date prepared: 2026-11-03

## Cause

The original V1.8, V1.9, V1.10 and V1.11 review/freeze records and three referenced validation scripts listed in V1.11 Appendix C cannot be located in the current repository or reachable Git history. The user confirmed that the eleven originals are unavailable and should be treated as lost. Their original contents, authorship, dates, decisions, hashes and evidence lists cannot be reconstructed as historical facts.

## Proposed change

Keep the eleven original evidence-register entries marked `missing` with no invented hashes, reviewers, dates or source links. Amend NFR-038 and Appendix C in a proposed V1.12 so the requirement can be satisfied by a clearly identified recovery package created now, without representing it as the missing historical originals.

The recovery package must contain:

1. A machine-readable loss register listing each missing item, its V1.11 source reference, failed retrieval scopes, and the user-confirmed loss disposition.
2. A new independent technical review of the complete V1.11 source snapshot, including its SHA-256, normative requirements, Appendix B, and the current contract/model coverage. Record the actual reviewer, review date, result, findings, evidence paths and hashes.
3. A new current freeze decision that identifies V1.11's source hash, the CR-002 disposition, unresolved historical claims, and the exact evidence reviewed. It must not imply that missing V1.8-V1.10 originals were inspected.
4. Rebuilt versions of the three referenced validation scripts, each labeled as a new implementation, with source references, tests, and an explicit statement that it does not reproduce the lost historical script.
5. A requirements-map and changelog update for the proposed NFR-038/Appendix C change, with all other normative requirements unchanged unless separately enumerated.

V1.12 may become effective only after the change is reviewed and approved under V1.11 sections 0.3-0.4. The new review and freeze records must carry their real creation dates and reviewer identities. Until then, V1.11 remains the frozen baseline and NFR-038 remains blocked.

## Impact

- Requirements: NFR-038 and the V1.11 Appendix C delivery-state rows.
- Acceptance: replace the impossible requirement to recover lost originals with verifiable current review/freeze evidence plus explicit historical-loss disclosure. The new package must still reject a pending or failed current review.
- Scope: no product behavior, security invariant, contract content, billing rule, or other V1.11 normative requirement changes.
- Phase 1: remains blocked until the change is approved, the recovery package is complete, and the new baseline validators pass on the same clean commit locally and in GitHub Actions.

## Approval

| Reviewer | Date | Decision | Evidence |
|---|---|---|---|
| User / specification owner | pending | pending | pending |
| Independent technical reviewer | pending | pending | pending |

This proposal is not an approved specification change. No original evidence row may be changed to `verified` on the basis of this proposal.
