# Phase 4 Actions Evidence

**Run:** `41728267582`
**Commit:** `7899188e757a83d2ed9ec2c226f2e7ddb460cb02`
**Workflow:** `phase4-m2-multi-hop`
**Date:** 2026-11-04

## Host

- Runner: GitHub Actions `ubuntu-24.04`
- OS: Ubuntu 24.04.5 LTS
- Architecture: amd64 (`x86_64`)
- Go: 1.27.3
- Docker Engine: 28.0.4
- Worktree: `dirty=false`

## Results

- `make phase4-m2-check`: passed.
- Engine race suite: passed.
- Targeted Phase 4 state-machine and VLink tests: passed.
- Docker VLink topology smoke: passed.
- `make phase4-m2-benchmark`: workflow succeeded, but benchmark metadata is `environment-limited` with `benchmark_exit: 1`; this is not a native performance qualification.

The benchmark gap remains open until a dedicated native Linux three-relay environment produces the required NFR-003/NFR-005/NFR-007 evidence and D-04 review.
