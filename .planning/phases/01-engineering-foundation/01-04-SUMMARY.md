# Phase 1 Plan 04 Summary

## Outcome

Created the shared Phase 1 verification path used by local Docker and GitHub Actions. Both invoke the exact command `make phase1-check`. The final test image is `FROM scratch` and contains only the statically linked network fixture executable.

## Artifacts

- `Makefile`
- `Dockerfile.phase1`, `compose.phase1.yaml`
- `.github/workflows/phase1.yml` with full commit SHA action pins and read-only permissions
- `tools/phase1/check.sh`, `tools/phase1/netns-smoke.sh`
- `test/netns/fixture/main.go`, `test/netns/compose.yaml`
- `docs/governance/verification-evidence.schema.json`
- `artifacts/phase1/evidence.json`

## Local Verification

On clean commit `d09cdc47ff2d8e6b61ff4b41ecf48620eb6a0680`, `make phase1-check` passed in a clean local clone using OrbStack Docker on arm64. The evidence records image digest `vortex-phase1@sha256:538155af9b3855f1cf2e088ef5a0b47f301dd2cc1539655d573b5fb178eac75d`, all required runnable checks, and the network fixture exchange/cleanup. NFR-038 remains explicitly blocked.

`make phase1-release-gate` remains non-zero because NFR-038 is blocked. The network fixture is smoke coverage only and does not claim Engine throughput, UDP correctness, leak, or production-scale acceptance.

## CI Status

GitHub Actions run [41584401538](https://github.com/VortexWang/Vortex/actions/runs/41584401538) passed on the same clean commit `d09cdc47ff2d8e6b61ff4b41ecf48620eb6a0680` using Ubuntu 24.04.5 amd64. The archived evidence records Docker image digest `sha256:dde6b31c927f126fdd19bfb0dcb83608ac4de26076e3d3f399f9e3a7015d2b4d`, `dirty: false`, and all runnable checks passing. The evidence-register check correctly records NFR-038 as blocked. Follow-up fixes `3503713` and `d09cdc4` made missing governance inputs fail/report explicitly and captured the initial worktree state before refreshing tracked evidence.
