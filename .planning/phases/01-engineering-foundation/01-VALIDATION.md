---
phase: "01"
slug: engineering-foundation
status: validated
nyquist_compliant: true
wave_0_complete: true
created: "2026-11-03"
---

# Phase 01 — Validation Strategy

> Current owner decision supersedes validation rows below that require NFR-038 historical evidence recovery. The 11 originals remain missing; recovery is not a gate. See `docs/governance/project-restart-decision.md`.

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Python standard library `unittest`, Go standard library model checks, Docker Compose, Make |
| **Config file** | `compose.phase1.yaml` and `Makefile` (Wave 0 creates both) |
| **Quick run command** | `make phase1-quick` |
| **Full suite command** | `make phase1-check` |
| **Estimated runtime** | quick <30 seconds; full <5 minutes, subject to approved finite model bounds |

---

## Sampling Rate

- **After every task commit:** Run `make phase1-quick`
- **After every plan wave:** Run `make phase1-check`
- **Before `$gsd-verify-work`:** Full suite must be green except the explicitly blocked NFR-038 evidence check
- **Max feedback latency:** 30 seconds for quick checks

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 01-01 | 01 | 1 | ENGR-001, ENGR-002, NFR-038, SUP-STACK-001, SUP-PLAT-001, SUP-TOOLS-001, SUP-PARAM-001 | T-01-01 / T-01-SC | Only approved, integrity-checked and license-allowed tools/images enter the lock; absent historical evidence remains blocked | unittest + audit | `make phase1-quick` | ✅ | ⚠️ partial: 43 locks pass; 7 candidates and NFR-038 remain blocked |
| 01-02 | 02 | 1 | ENGR-004, SUP-FMT-001, SUP-IFACE-001, SUP-STRUCT-001 | T-01-03 / T-01-07 / T-01-SC | Every requirement maps to a versioned, hash-reviewed contract or a reasoned not-applicable record | schema + mapping tests | `make phase1-quick` | ✅ | ✅ frozen contracts and 277-ID map pass |
| 01-03 | 03 | 1 | ENGR-006, SUP-B1-001, SUP-B2-001, SUP-B3-001, SUP-B4-001, SUP-B5-001 | T-01-04 / T-01-SC | Go state explorer rejects violations of quota conservation, unique active generation, one-time transfer, idempotent usage and conservative unknown liability | model/property tests | `make phase1-model` | ✅ | ✅ finite model tests pass; bounds/pairs reviewed by user |
| 01-04 | 04 | 2 | ENGR-003, ENGR-005, SUP-MILE-001, SUP-TEST-001, SUP-ARTIFACT-001 | T-01-05 / T-01-06 / T-01-SC | Local Docker and Actions invoke the same command and retain commit-bound evidence | CI integration | `make phase1-check` | ✅ | ✅ same clean SHA passes local arm64 and Actions amd64 |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `Makefile` — `phase1-quick`, `phase1-model`, and `phase1-check` targets
- [ ] `compose.phase1.yaml`, `Dockerfile.phase1` — static `scratch` network-fixture container; Python/Go checks use pinned host toolchains
- [ ] `tools/phase1/` — requirement, contract and governance validators with standard-library tests
- [ ] `model/` — deterministic Go state model, coverage map and result collector
- [ ] `.github/workflows/phase1.yml` — action SHAs pinned and `make phase1-check` invoked unchanged
- [ ] `test/netns/` — namespace fixture and smoke harness; no Engine performance pass claimed before M0

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Approve exact candidate versions, digests and license findings before the lock is written | ENGR-001, ENGR-002 | The user owns the explicit baseline approval decision | Review the generated candidate manifest and approve or reject each unresolved/exceptional entry at the blocking checkpoint |
| Approve all five contract contents and source mappings before they are marked frozen | ENGR-004 | The normative contract is a downstream compatibility boundary | Review the five artifact hashes, V1.11 anchors and initial versions at the blocking contract checkpoint |
| Confirm NFR-038 evidence search and blocked status are represented accurately | NFR-038 | The historical review/freeze records are external evidence and cannot be recreated by code | Inspect the evidence register, referenced paths and search notes; confirm no missing item is described as passed |
| Review deterministic model abstraction and pairwise fault inclusion/exclusion rationale | ENGR-006 | Model scope and risk prioritization require domain judgment | Review the state/requirement coverage matrix, invariant list, pairwise scenarios and stated finite bounds |

---

## Validation Sign-Off

- [x] All tasks have automated verification or documented manual-only gates
- [x] Sampling continuity: each plan has an automated verification command
- [x] Wave 0 covers all missing test references
- [ ] No watch-mode flags
- [ ] Feedback latency <30s for quick suite
- [x] `nyquist_compliant: true` set in frontmatter after validation

**Approval:** partial; automated checks are covered. Candidate closure and NFR-038 remain blocked manual-only gates.

## Validation Audit

- 2026-11-03: Audited 4 plan groups. Contract, bounded-model and dual-host groups pass; dependency/license closure is partial with 7 unresolved candidates. NFR-038 remains blocked.
