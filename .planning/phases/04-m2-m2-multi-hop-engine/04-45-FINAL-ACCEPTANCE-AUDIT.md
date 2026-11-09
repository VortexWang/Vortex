---
phase: 04-m2-m2-multi-hop-engine
status: passed
source_sha: 0c2ba71c0653ca1c806dc3df1db93d847544011b
engine_tree: dacb5480c65c5563ed9a954482766b270730263d
---

# Phase 4 final acceptance audit

## Effective scope

The objective is to finish Phase 4 against its actual stage boundary, not to
keep stale debts open indefinitely. `04-CLOSURE.md` and `04-CONTEXT.md` define
Engine/tracer plus injected process-local authority. X5/X6 production durable
authority is a handoff, not an unmet Phase 4 implementation requirement.

Host eligibility uses the explicit baseline correction `6d3d36a3` and quick
`20261105-qualification-scope-correction`: Ubuntu 24.04, at least four logical
CPUs and 8 GiB RAM, native execution and a non-loopback interface. Dedicated
resources/link and provider attestation are informational, not mandatory. Older
claims to the contrary in historical evidence are superseded by that decision.

Measured source is clean `0c2ba71`; the current branch has the identical Engine
tree `dacb5480c65c5563ed9a954482766b270730263d`. Later audit/docs changes do not
create a new measured source identity. Cross-host reports retain actual config
and semantic hashes; fifteen-second Docker and hosted CI are regression only.

## Requirement-to-evidence audit

| Item | Required proof | Inspected evidence and result | Conclusion |
| --- | --- | --- | --- |
| Q1 runtime/topology/authority | Real source, three relays, destination, TLS; bounded funded authority | Native 30% candidate generated configs and source credit; source snapshot `phase4-formal/source/path-2/policy-2`, finite 1 TiB budget, both directions gated; runtime reports pass | Technical requirement met |
| Q1 host | Effective minimum native host baseline | Trial host Ubuntu 24.04.5, x86_64, 12 CPUs, 31.3 GiB, native-host, interface enp3s0f0, GOMAXPROCS=1; worktree clean | Met under corrected baseline; no per-node exclusivity claim |
| Q1 formal duration/load | >=60 s warmup, 3 x 300 s, sixteen TCP and actual 100 Mbps UDP | Explicit 300 s warmup; trials 628.675400 / 631.912733 / 636.537656 Mbps TCP; UDP 99.998368 / 99.998112 / 99.997408 Mbps, zero loss, credit conserved and settled | Met |
| Q1 same-load independent TLS | >=400 Mbps and runtime/TLS >=70%, same clean SHA and semantic workload | TLS 568.820579 Mbps, ratio 1.119048221; clean 0c2ba71 and semantic 84a3e5c9e6efe6b8496ba6cd1a33fe92ecf9d5cad3c4e0182a14e6a03fec2de6; native and independent local gate pass | Met |
| Q1 same-source regression | Docker and Actions separately recorded | Local detached clean 0c2ba71 Docker correctness pass; Actions 42308608919 success with all 15 host reports clean 0c2ba71 | Met; neither regression used as formal performance proof |
| Q2 finite threat/config matrix | Identity/revision/path rejects, binary integrity, bypass and current-route faults | Current formal comparison includes forged OPEN, relay-2/destination bypass rejection, 64 MiB binary integrity and active middle failure; same-source Actions recovery eight cases; see 04-43 and compare_results.py's checked inputs | Met for enumerated current configuration |
| Q3 policy | Conservative T boundary, preissue/lease coverage, pause/recovery, hard cutoff | Accounting policy tests enforce T-60, 300 s window, exact-T selection, uncertainty, <=12 s protection band, old unknown preservation and rollback rejection; native 2543fb7 accounting suite passes, Engine tree identical | Met at injected Engine boundary |
| Q3 shares | CAS/idempotency, overlap/drain, lease expiry, fencing | engine/share tests cover prepare/commit history, overlap max, 50->60->40 semantics and boot fence; ShareGate tests prevent open/write past lease and retain unresolved bounds; existing Engine race and Actions shared checks pass | Met at injected Engine boundary |
| Q3 snapshots/source | Immutable affinity, late/lost response isolation, per-source conservation | Same-source Actions snapshot and finite TCP/UDP source-credit artifacts pass; accounting book/grant/policy tests inspect old/new and unknown isolation | Met at stage boundary |
| Q4 candidate scan | 64/256/1024 KiB x 10/30/60 s x 20/30/50%; three repeats, short/16TCP/UDP | Native 2543fb7 full 81-trial matrix; independent raw counters/config/timeline/target formula/65 s idle audit pass; 27-row comparison in 04-44 | Met |
| Q4 dynamic/safety | Committed-consumption target, bounded merge/wait, timeout/cancel/reject/unknown, idle safety | Native sampled formula/idle audit; delayed/renewal/demand/credit tests inspect prewrite, coalescing, 200 ms deadline, late acceptance exactly once, frozen unknown, only idle-tail reclaim | Met; no issuer grant return claim |
| Q4 stress limitations | Preserve failure, do not expand deadline to force pass | All nine 600 ms renewal trials fail availability; resets/UDP loss retained; 200 ms wait unchanged | Limitation recorded; no 600 ms availability claim |
| Q4 final decision | Concrete reviewed parameters and owner lock confirmation | 256 KiB initial, 4 MiB max, 8 MiB floor, 30 s horizon capped at 375000000 bytes, 30% waterline, 200 ms wait. Three center-profile formal trials and TLS pass; owner explicitly replied “确认” to this table; recorded in 04-D04-LOCK.json | Met and locked for Phase 4 |
| Q5 controlled RTT/modes | Preserve passed data-path identity or rerun changed path | Native edb1849c pool/mux 50 samples each, additional median 0.991130/1.049556 ms; auto observed both. Engine tree identical at edb1849c/2543fb7/0c2ba71/current; same-source CI modes pass | Met; persistence latency excluded |

## Required artifacts and checks

The canonical deliverables `04-06-D04-REVIEW.md`,
`04-06-QUALIFICATION-MATRIX.md`, and `04-11-EVIDENCE.md` now reference the full
matrix, distinct candidate profiles and explicit limits. `04-43` retains source
identities; `04-44` contains every comparison and the proposed parameter table.
`04-CLOSURE`, `04-VERIFICATION`, ROADMAP and STATE record the final
passed verdict following the explicit parameter confirmation. Earlier failures and
stale historical statements remain labeled historical, not silently rewritten.

Local 37 Phase 4 Python tests, full matrix raw auditor, both formal gates and
`git diff --check` pass. Prior Go test/vet/race and native accounting evidence
remain valid for the unchanged Engine tree; this document-only pass does not
pretend to be a new native race run.

Phase 1 frozen-contract hash and Phase 5 full-disk CI failures remain separate
records. The draft CI PR is not merged; Phase 4 qualification does not imply
product release, production PostgreSQL authority or all-phase CI success.

## Completion decision

The owner replied “确认” to the proposed parameter table on 2026-11-07.
That resolves the final Q4 decision; all five Phase 4 qualification items now
pass within the effective Engine/tracer boundary. The confirmed values and
measurement scope are recorded in `04-D04-LOCK.json`. Historical failures and
X5/X6 handoffs remain unchanged. No required Phase 4 work remains.
