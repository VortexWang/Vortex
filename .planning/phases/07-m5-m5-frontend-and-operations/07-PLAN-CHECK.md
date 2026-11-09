# Phase 7 Independent Plan Check

## ISSUES FOUND

**Phase:** M5 前端与运营能力  
**Plans checked:** 35 / 84 tasks / 34 waves  
**Issues:** 3 blockers, 4 warnings, 0 info  
**Scope:** Planning review only. No installation, application execution, runtime tests, implementation, historical verification changes, or STATE writes.

Goal: 中文用户和运营人员可理解并管理授权业务状态，DNS、审计、通知和恢复操作反映系统真实执行事实。

## Blockers

1. **Research resolution:** `07-RESEARCH.md:728–735` retains six unmarked open questions: TS7/generator locking, legitimacy degradation, independent deployment authority/storage/custody, exact SemVer/approval granularity, runtime resources/evidence, and UI-SPEC/gate. Candidate and approval tasks provide useful future gates, but the research-resolution check cannot treat this unresolved section as resolved. Resolve the planning choices and their dispositions without claiming unrun compatibility probes, pending owner approvals, or runtime acceptance have passed. Renaming the heading alone does not establish resolution.

2. **Per-plan scope:** 16 plans reach the checker’s blocking threshold of 15 modified files. Exact frontmatter counts: 07-01=15, 07-02=15, 07-03=19, 07-04=16, 07-05=16, 07-06=15, 07-07=15, 07-09=15, 07-11=26, 07-14=21, 07-17=15, 07-18=16, 07-19=16, 07-20=15, 07-21=16, 07-22=19. Counts include explicitly declared generated outputs. These outputs must remain owned and generated alongside their authoritative changes; removing them from metadata is not a remedy. Re-slicing must preserve tracer-first delivery, all 27 requirements and 32 decisions, and candidate-before-approval ordering.

3. **SEC-005 sensitive-operation limiting:** 07-03-T03 implements account/source limits for login, TOTP and recovery. 07-05-T01 introduces password/TOTP step-up but specifies no attempt budget or exhaustion test. Credential writes/rotation (07-19), security/system settings (07-22), and approved recovery advancement (07-34) likewise have no explicit sensitive-operation rate-limit wiring or rejection test. Step-up reuse, permission checks, body limits and bounded workers do not implement the requirement that sensitive operations are rate limited.

## Warnings

1. **Borderline file scope:** 11 additional plans exceed the 5–8 file target and reach the warning band: 07-08=14, 07-12=14, 07-13=14, 07-15=13, 07-16=12, 07-23=14, 07-24=10, 07-25=10, 07-26=12, 07-28=10, 07-34=12. Their low-confidence token estimates do not override the file thresholds.

2. **Probe-policy bootstrap ambiguity:** 07-14-T01 says the real Agent probe reads the current destination policy, while T02 requires private-network exception and control-port rejection tests. The immutable policy is only constructed/approved/implemented in 07-23-T01–T03; 07-23 depends on 07-14, and all-production-path/probe enforcement expands in 07-24-T01. A strictly allowed public-target tracer may run before that expansion, so this is not a declared graph cycle. However, the plans do not identify the exact already-available policy/call path that makes the earlier probe safe, or assign its later full-policy re-verification to an explicit segment-probe case. Define that executable bootstrap boundary rather than relying on “current policy.”

3. **Pattern assignments:** Plans read PATTERNS.md and common router/tenantscope files, but task actions do not consistently identify their assigned analogs. Examples: 07-20 announcement service lacks its mapped `backend/internal/service/rule.go` analog; 07-21 notification worker lacks `backend/internal/worker/dns.go`; 07-28 backup files have no analog and do not cite the relevant RESEARCH.md physical-backup pattern in their actions. The copied constraints are useful, but the requested per-file pattern trace is incomplete.

4. **Browser/public security deployment:** RESEARCH.md’s ASVS 5 L1 mapping explicitly includes CSP, same-origin deployment and public TLS (V3/V12). 07-02 creates a test frontend Dockerfile; 07-35 creates test Compose. No task owns the browser-serving/public TLS/CSP deployment configuration or its negative test. 07-24-T02’s concrete TLS matrix covers backend↔Hub↔Agent and VLink, not the browser/public boundary. “ASVS Level 1” in every threat table does not supply this missing executable mapping. Reuse an existing secured public listener if appropriate, but explicitly wire the production SPA and validate its actual headers/origin/TLS behavior.

## Coverage and execution checks

| Area | Finding |
| --- | --- |
| Requirements | All 27 roadmap IDs appear in frontmatter; concrete work covers FE-001–012, SEC-001–013 and both SUP requirements, subject to SEC-005 gap above. Final-plan blanket claims were not used as sole coverage. |
| Locked context | All 32 decisions traced through source audit, truths and task actions; no deliberate removal of locked scope or implementation of deferred dark theme/payment/team/cloud adapters found. |
| Task structure | `verify.plan-structure` returned valid=true/exit=0 for all 35 plans. Each task includes files/action/verify/done; tracer type is accepted by the installed structural checker. |
| Dependencies | Declared references exist, are acyclic and satisfy wave minima. Shared generated contracts are ordered. Only 07-24 and 07-27 share wave24; their declared write sets do not overlap and no specific shared mutable writer/reader resource requiring an undeclared ordering was found. |
| Wiring / tiers | Actions connect browser→REST→service/PG and runtime observations/probes→authenticated Agent/Engine evidence. Financial calculation remains backend-owned; Engine remains independent and uses process-local interfaces. Some key-link metadata is schematic/self-referential, but concrete wiring is in the actions. |
| Create-before-use | 07-01 creates its Python candidate checkers before their commands; 07-02-T02 creates check.sh/runner before use. Subsequent browser tracers may use precreated spec tags under 07-02-T03’s explicit fallback; suite modules are created/extended by their owning plans. New files being absent now is not a finding. Commands use `sh`/`python3`, so new script executable bits are not prerequisites for those commands. Probe-policy prerequisite remains the warning above. |
| Approval ordering | 07-01, 07-23 and 07-27 construct concrete candidate bytes/diffs/hashes before owner checkpoints. No request for blanket approval is required before constructing those reviewable candidates. |
| Runtime truth | Bootstrap temporary sessions are explicitly excluded from final login acceptance. Password/TOTP, CRUD, state, measurements, notification/DNS and recovery tasks require real backend/PG/Agent/Engine evidence; no mock substitute is accepted as final delivery. |
| Physical recovery | 07-28/29 require real basebackup+continuous encrypted WAL, clean target, target LSN, post-base M1/M2 markers, promotion/new timeline and M3 fork; corruption/identity/gap failures are explicit. |
| Fencing / funds | 07-30 requires independent durable journal plus separately trusted anchor, rollback/concurrency/partition evidence and no new issuance without fencing. 07-31–33 require Hub FULL, unique same-R transfer, immediate original-scope renewal, finite K plus continuous tail, PG/WAL entitlement facts, per-source F and per-tenant release without extending the original hard deadline. |
| Security threats | ASVS5 L1/block-critical-high and actual negative tests are stated throughout. This review does not credit those declarations as implemented mitigation. Sensitive limiting and browser/public deployment are the gaps above. |
| Evidence | Final plan requires the same clean implementation SHA across local Docker and native Actions amd64/arm64, test counts, no skips, original failures, real CF test zone, and human custody/mobile evidence. Phase4/8 qualification remains separate. |
| Edges/prohibitions | 53 edges: 51 explicit resolved dispositions; E-07-52/53 remain visibly unclassified and assigned to 07-35. Four descriptorless prohibitions remain flagged-unverified; not falsely green or considered wired mechanical checks. |
| AGENTS | No project skills directories were found. Plans preserve SQL-first/append-only migration, scope/audit/outbox, dependency approval/license, architectural and evidence constraints. No historical STATE/verification rewrite is proposed. |

## Nyquist and supplied deterministic probes

VALIDATION.md exists; every task has automated feedback and a stated failing direction, with no three-task unverified window or MISSING sentinel. Real browser/physical recovery latency remains unmeasured; plans explicitly separate long drills and selected fast subsets rather than claiming measured timings.

Both supplied JSON documents were read; neither was rerun or treated as executing tests. Path probe: 84 rows, all `not_applicable`, severity `none`, readError null. This means the recognizer did not resolve these Python/shell-wrapper targets; it does **not** prove them runnable. Failure-direction probe: 84/84 `ok`, severity `none`, readError null. Statements name concrete observable rejected outcomes, not merely “the command fails.” No probe-derived blocker/warning is added.

## Smart-zone estimates

`estimate-check --calibrated` was run per plan. Budget=100000; all estimates are within budget, all confidence=low with sample_count=0 and factor=1. They are uncalibrated project estimates, not precise execution measurements. No token-budget warning; file-scope findings still apply.

| Plans | Estimated tokens per plan | Budget | Over budget |
| --- | --- | --- | --- |
| 07-01, 07-27 | 22000 | 100000 | false |
| 07-02–07-26 | 24000 | 100000 | false |
| 07-28–07-35 | 30000 | 100000 | false |

## Structured Issues

```yaml
issues:
  - plan: null
    dimension: research_resolution
    severity: blocker
    required_property: "RESEARCH.md carries no unresolved planning question; future approval and runtime acceptance remain explicitly unverified gates."
    description: "07-RESEARCH.md:728–735 has six Open Questions with neither a RESOLVED section suffix nor individual resolution/disposition: TS7 generator, legitimacy, deployment authority/custody, SemVer/approval, runtime resources/evidence, UI-SPEC/gate."
    fix_hint: "Have the researcher resolve the planning choices against the current candidate/probe/approval plans and record remaining execution gates honestly; do not claim empirical success or apply a heading-only rename."
  - plan: null
    plans: [07-01, 07-02, 07-03, 07-04, 07-05, 07-06, 07-07, 07-09, 07-11, 07-14, 07-17, 07-18, 07-19, 07-20, 07-21, 07-22]
    dimension: scope_sanity
    severity: blocker
    required_property: "Each execution plan stays below the checker's 15-file blocking threshold while preserving ownership of all actual outputs."
    description: "Declared files_modified counts are respectively 15,15,19,16,16,15,15,15,26,21,15,16,16,15,16,19; generated outputs are actual declared modifications, not exempt from the threshold."
    fix_hint: "Re-slice into bounded tracer-plus-expansion plans, keeping generation with its authoritative source changes, all requirements/decisions, and valid ordered write ownership."
  - plan: 07-05
    plans: [07-03, 07-05, 07-19, 07-22, 07-34]
    task: 1
    dimension: requirement_coverage
    severity: blocker
    required_property: "SEC-005 has executable rate-limit enforcement and exhaustion/failure tests for sensitive operations as well as login."
    description: "07-03-T03 names login/TOTP/recovery limits only; step-up07-05, DNS credential writes07-19, security/settings07-22 and recovery advancement07-34 contain authorization/replay/ETag checks but no sensitive-operation request budgets or rate-limit rejection tests."
    fix_hint: "Define and wire a bounded current-identity/source/operation sensitive-action policy, with concurrent exhaustion and unavailable-limit-store tests; preserve step-up, confirmation and permission checks."
  - plan: null
    plans: [07-08, 07-12, 07-13, 07-15, 07-16, 07-23, 07-24, 07-25, 07-26, 07-28, 07-34]
    dimension: scope_sanity
    severity: warning
    required_property: "Per-plan file scope stays near the 5–8 file target and below the 10-file warning band where practical."
    description: "These plans declare respectively 14,14,14,13,12,14,10,10,12,10,12 modified files; all estimates have low confidence and zero calibration samples."
    fix_hint: "Consider bounded additional slices while preserving complete vertical behavior and generator ownership."
  - plan: 07-14
    plans: [07-14, 07-23, 07-24]
    dimension: dependency_correctness
    severity: warning
    required_property: "An earlier probe tracer names an available safe policy path and later full-policy integration is explicitly verified without a backwards prerequisite."
    description: "07-14-T01 reads 'current target policy' and T02 covers private exceptions/control ports; immutable policy is only approved/implemented in07-23, which depends on07-14, and probe-path enforcement expands in07-24. The bootstrap policy and segment-probe revalidation boundary are not specified."
    fix_hint: "Specify the existing safe public-target tracer policy and an explicit post-policy segment-probe case, or reorder a bounded policy prerequisite; do not weaken destination restrictions."
  - plan: null
    plans: [07-20, 07-21, 07-28]
    dimension: pattern_compliance
    severity: warning
    required_property: "Applicable task actions identify their PATTERNS.md analogs, or cite the relevant RESEARCH.md pattern when no analog exists."
    description: "Announcement-service actions omit mapped backend/internal/service/rule.go; notification-worker actions omit backend/internal/worker/dns.go; backup actions have no analog and no explicit relevant research-pattern citation. Generic read_first entries do not provide this per-file trace."
    fix_hint: "Name the assigned analog/research section and the relevant transaction/worker/physical-backup pattern in the corresponding actions."
  - plan: 07-02
    plans: [07-02, 07-24, 07-35]
    dimension: key_links_planned
    severity: warning
    required_property: "The researched ASVS L1 browser/public transport boundary has owned deployment configuration and executable regression checks."
    description: "Research V3/V12 requires CSP, same-origin and public TLS; plans own only frontend test Docker/Compose while07-24-T02 explicitly tests component mTLS and VLink. No task wires the served production SPA/public listener security configuration or validates its headers/origin/TLS."
    fix_hint: "Explicitly reuse/configure the existing secured public listener or a reviewed deployment boundary for the SPA, then verify actual public transport and browser security headers."
```

## Recommendation

Revise the three blocking properties and four warning properties before execution. None of these findings authorizes changing locked decisions, silently dropping outputs, or reporting approval/runtime evidence that has not occurred.
