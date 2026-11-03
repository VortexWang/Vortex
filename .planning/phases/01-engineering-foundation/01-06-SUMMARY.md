---
phase: 01-engineering-foundation
plan: 06
status: partial
completed: 2026-11-03
gap_ids: [G-01-02]
---

# Plan 01-06 Summary: NFR-038 Recovery Proposal

The user confirmed that all eleven historical records/scripts listed by V1.11 Appendix C are unavailable and should be treated as lost, and requested replacement evidence. The repository tree and Git history reachable from `main` and `origin/main` also contain none of those originals.

Recorded that user statement in `docs/governance/evidence-register.yaml` and `.planning/research/nfr038-search-2026-11-03.md`. Prepared `docs/governance/proposals/CR-002-NFR-038-recovery.md`, proposing a transparent present-day independent re-review and replacement evidence package under a new V1.12 baseline. The proposal explicitly prohibits backdating or representing replacement artifacts as historical originals.

No original register row was marked verified. `python3 tools/phase1/baseline.py evidence` correctly returns `blocked`. CR-002 is still a draft and requires formal user/specification-owner approval and an independent technical review before any V1.11 requirement or Appendix C change takes effect. NFR-038 and Phase 1 remain blocked.

Verification: 12 Phase 1 Python tests pass; GSD consistency passes; evidence checker remains blocked as required. This plan is blocked pending formal CR-002 approval and completion of the replacement review package.
