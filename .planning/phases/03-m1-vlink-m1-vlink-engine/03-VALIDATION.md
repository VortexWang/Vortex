---
phase: "03"
slug: "m1-vlink-m1-vlink-engine"
status: draft
nyquist_compliant: false
wave_0_complete: false
created: "2026-11-03"
---

# Phase 03 - Validation Strategy

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing`, `go test -race`, fuzz tests, Docker test script |
| **Config file** | `engine/go.mod`; new Phase 3 test entry and Docker script during Wave 1 |
| **Quick run command** | `cd engine && go test ./...` |
| **Full suite command** | `make phase3-vlink-check` |
| **Estimated runtime** | ~60 seconds locally; benchmark and Docker evidence separate |

## Sampling Rate

- After every task commit: `cd engine && go test ./...`
- After every plan wave: `make phase3-vlink-check`
- Before `$gsd-verify-work`: full suite plus Docker and GitHub Actions evidence on the same SHA
- Max feedback latency: 90 seconds for the quick command

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 03-01-01 | 01 | 1 | ENG-012, ENG-013 | T-03-01 | bounded fixed codec rejects malformed frames before allocation | unit/fuzz | `cd engine && go test ./vlink -run 'Test(Frame|Codec)'` | ✅ | ✅ |
| 03-01-02 | 01 | 1 | ENG-011, ENG-015 | T-03-02 | HELLO canonical bytes and exporter binding are deterministic | unit | `cd engine && go test ./vlink -run 'TestHello'` | ✅ | ✅ |
| 03-01-03 | 01 | 1 | ENG-012, ENG-013, ENG-014 | T-03-03 | one authorized TCP OPEN/DATA/FIN tracer succeeds | integration | `cd engine && go test -race ./vlink -run 'TestTracer'` | ✅ | ✅ |
| 03-02-01 | 2 | 2 | ENG-010, ENG-025 | T-03-04 | mTLS identity, conservative time bounds, freshness and stale replay are rejected | unit/integration | `cd engine && go test ./vlink -run 'TestIdentity|TestFreshness'` | ❌ W0 | ⬜ |
| 03-02-02 | 2 | 2 | ENG-016 | T-03-05 | stream/link failure isolation and byte windows preserve bounds | unit/race | `cd engine && go test -race ./vlink -run 'Test(Stream|Window)'` | ❌ W0 | ⬜ |
| 03-02-03 | 2 | 2 | ENG-016, ENG-019 | T-03-05 | byte windows and 3/3/12 second liveness bounds hold | unit/race | `cd engine && go test -race ./vlink -run 'Test(Window|Liveness)'` | ❌ W0 | ⬜ |
| 03-03-01 | 3 | 3 | ENG-020, ENG-021, ENG-022, ENG-023, ENG-024 | T-03-06 | OPEN checks local authorization and UDP requires OPEN_OK | integration | `cd engine && go test ./vlink -run 'Test(UDP|Open|Target)'` | ❌ W0 | ⬜ |
| 03-04-01 | 4 | 4 | ENG-017, ENG-018 | T-03-07 | mode selection, reverse links and pool retries are bounded and cleaned up | integration/race | `cd engine && go test -race ./vlink -run 'Test(Pool|Mode|Reverse|Cleanup)'` | ❌ W0 | ⬜ |
| 03-05-01 | 5 | 4 | — | T-03-08 | abuse controls close sustained offenders and metrics stay low-cardinality | unit | `cd engine && go test ./vlink -run 'Test(Limit|Observability)'` | ❌ W0 | ⬜ |
| 03-06-01 | 6 | 5 | NFR-002, NFR-004, NFR-007 | T-03-09 | forward/reverse and first-packet measurements are reproducible and honestly reported | Docker/benchmark | `make phase3-vlink-check` | ❌ W0 | ⬜ |

## Wave 0 Requirements

- [ ] `engine/vlink/` package skeleton and test fixtures
- [ ] fixed local TLS certificate fixture generator or test certificates
- [ ] `tools/vlink/docker-test.sh` and `make phase3-vlink-check`
- [ ] `.github/workflows/phase3.yml` using the same command and pinned action SHAs

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Dedicated hardware throughput and first-packet NFR qualification | NFR-002, NFR-004, NFR-007 | Hosted CI and Docker are not the specified dedicated performance environment | Run the recorded benchmark on the approved Ubuntu 24.04 native host; attach raw results, host identity, commit SHA and tool versions |

## Validation Sign-Off

- [ ] All tasks have automated verification or an explicit Wave 0 dependency
- [ ] No three consecutive tasks without automated verification
- [ ] Wave 0 covers missing test infrastructure
- [ ] No watch-mode flags
- [ ] Feedback latency target is under 90 seconds for quick checks
- [ ] `nyquist_compliant: true` after implementation and validation audit

**Approval:** pending
