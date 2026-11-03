# Phase 1 Plan 01 Summary

## Outcome

The approved candidate snapshot was recorded and a partial immutable lock was created from the user-approved manifest digest. The lock contains only candidates with verified version, source, integrity, and allowlisted license evidence.

## Artifacts

- `docs/governance/version-candidates.yaml`: 50 candidate records; approved snapshot digest `3444057c66ea80de0eb94517c51119f737707237b10b1bbd1d586513f5164060`.
- `docs/governance/version-lock.yaml`: 43 locked records and 7 explicit `not_locked` records.
- `docs/governance/dependency-closure-review.json`: review evidence for 202 Go module records and 256 npm package records; not a project lockfile.
- `tools/phase1/baseline.py`: added `check-lock` validation for digest, source, integrity, license, and lock membership.

## Verification

- `python3 -m unittest discover -s tests/phase1 -p 'test_baseline.py'`: passed, 6 tests.
- `python3 tools/phase1/baseline.py check-lock`: passed, 43 locked / 7 not locked.
- `python3 tools/phase1/baseline.py evidence`: blocked as required by NFR-038.
- `python3 tools/phase1/baseline.py candidates`: blocked by unresolved dependency/image/license closure gaps.

## Open Gates

The 7 not-locked records are PostgreSQL/Valkey image package closure, SQLite embedded build attestation, the SWC composite license classification, host Docker provenance, scratch topology image digest, and network fixture checksum. These remain release blockers. No project dependency installation or generated Go/pnpm lockfile was performed in this plan.
