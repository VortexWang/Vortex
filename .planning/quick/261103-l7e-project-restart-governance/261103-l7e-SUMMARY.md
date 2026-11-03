---
id: 261103-l7e
slug: project-restart-governance
status: complete
completed: 2026-11-03
---

# Quick Task Summary: Project Restart Governance

Recorded the owner decision that V1.11 is reference only, the eleven historical files cleared at restart will not be recreated, and NFR-038 recovery is out of scope. Original register entries remain `missing`; the evidence check reports `out-of-scope`, never `passed`.

Recorded PostgreSQL and Valkey as external data services. Their server image OS package license closure is outside the VORTEX product dependency gate; versions, provenance, image digests, and integration compatibility remain tracked. Remaining product dependencies are checked before first use.

Updated GSD project, roadmap, requirement, Phase 1 verification, UAT, security, validation and historical plan records. Phase 1 is complete for M0 entry; Phase 2 is ready for discussion.

Verification passed:

- `python3 -m unittest discover -s tests/phase1 -p 'test_*.py'` (14 tests)
- `python3 tools/phase1/baseline.py evidence` (`out-of-scope`)
- `python3 tools/phase1/baseline.py check-lock`
- `python3 tools/phase1/contracts.py check-frozen`
- `python3 tools/phase1/contracts.py check-map`
- `go test ./model ./tools/phase1 ./tests/phase1 ./cmd/modelcheck`
- `make phase1-release-gate` (9 checks, Docker build and network smoke)
- GSD consistency and verification status pass

Local evidence records OrbStack arm64, commit `62f46872eb1d097392a84dae51fcd6da81c97605`, and `dirty=true`. The earlier clean same-commit Actions evidence remains unchanged; Actions has not yet run against this governance update.
