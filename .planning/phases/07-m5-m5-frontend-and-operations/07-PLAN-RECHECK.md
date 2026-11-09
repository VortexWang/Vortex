# Phase 7 Independent Plan Recheck — Iteration 2/3

## ISSUES FOUND

**Phase:** M5 前端与运营能力  
**Plans checked:** 84 plans / 168 tasks / 84 waves  
**Issues:** 2 BLOCKER, 0 WARNING, 0 INFO  
**Scope:** Pre-execution plan review. No installation, application execution, runtime tests, owner approval, implementation, historical verification modification, or STATE writes. Only this report is written.

Goal from ROADMAP: 中文用户和运营人员可理解并管理授权业务状态，DNS、审计、通知和恢复操作反映系统真实执行事实。

The revision materially improves the original seven findings. However, two executable task boundaries remain inconsistent. The plan set must not proceed as written: one task requires a browser application before its producing plan, and another requires browser confirmation changes without owning a browser source file or identifying an already implemented reusable confirmation path.

## Blocking properties

### R2-01 — Verification prerequisites must exist at the task's execution point

- **Plan/task:** 07-04-T02, action line 97; producing plan 07-05-T01, line 90.
- **Evidence:** 07-04 is the approved dependency installation/compile-probe slice. Its complete write set is package.json, pnpm-lock.yaml, tsconfig.json, vite.config.ts and Dockerfile.test. T02 nevertheless requires adding rejection tests to “本片真实页面”, issuing its REST requests, inspecting authoritative objects, preserving drafts and comparing two logged-in browser contexts. The application entry/index, first browser spec and check/runner are explicitly created by dependent 07-05. 07-04's command runs dependency_candidate_check --installed --fixture-branch rejections and tsc; it neither selects nor creates the specified page rejection tests. This is an actual future prerequisite, not the general observation that proposed files do not exist today.
- **Effect:** The executor must either perform later/unowned browser work, ignore mandatory action/acceptance language, or stop before 07-05. A copied rejection paragraph is not a valid second task for the installation slice.
- **Example remedy, non-binding:** Keep this slice's negative branch on concrete installed-dependency/strict-compile/integrity fixtures using an explicitly owned or previously created checker; place the complete browser/REST/draft negative requirements in the later producing browser slice. Preserve both installation rejection coverage and required browser behavior. Any mechanism reaching the property is valid.

### R2-02 — A browser tracer must own its wiring or identify an already created executable reusable path

- **Plan/task:** 07-39-T01, action line 99; related 07-38-T01 line 86 and 07-40-T01 line 86.
- **Evidence:** 07-39 requires a real browser rule-preview → final confirmation → Engine execution tracer: the editor confirmation area displays the real preview DTO and session count, and submission binds preview_id. Its nine declared paths comprise migration/query/service/HTTP, generated SQL/models/schema/version and impact-preview.spec.ts; no browser production source is owned. The preceding 07-38 explicitly leaves final confirmation for 07-39/07-40 and disables submission while unavailable. 07-40, which owns rule-editor.tsx and the impact_preview suite, depends on 07-39. No earlier task explicitly creates and wires a generic rule-preview confirmation renderer/API adapter capable of satisfying 07-39's exact tracer without source changes. The words “通用确认区” do not establish that prerequisite.
- **Effect:** This slice can pass only by an undeclared frontend write, by relying on a dependent plan's future implementation, or by replacing the required real browser tracer with a different test. The generated-output/file-scope repair is therefore not fully established simply by its nine-path count.
- **Example remedy, non-binding:** Explicitly own a bounded real editor wiring change alongside the first browser tracer, or create and verify the exact reusable preview/confirm integration in a prior slice; alternatively make the earlier slice honestly API-only and place the complete browser tracer with its source owner. Keep source/generation ownership accurate and stay within scope thresholds without hiding writes. No particular file split is mandated.

## Original seven findings: re-evaluation

| Original property | Revision evidence | Disposition |
| --- | --- | --- |
| Research resolution | RESEARCH “Planning Questions — RESOLVED” gives six actual choices and separate pending execution/failure gates: TS7/isolated TS5, SUS review, independent authority/anchor/custody, exact version/approval staging, native evidence, UI contract gate. | Resolved as a planning issue; no empirical result credited. |
| Blocking file scope | All 84 frontmatter write sets contain at most 9 paths; schema/SQL outputs and control/API complete generation remain declared and ordered. | Numeric threshold resolved; truthful complete ownership is still blocked by R2-02. |
| Sensitive operation limits | 07-08 defines PG persistent actor/operation/source/tenant budgets, attempt consumption before secret verification, 429/503, multi-instance N+1 exhaustion, session-rotation resistance, Valkey clear/offline and PG failure. 07-15, 07-55, 07-62 and 07-82 explicitly apply the same policy to step-up, credential rotation, password/security/system settings and recovery advancement. 07-06 supplies default-deny sensitive-route registration. | Original missing security coverage resolved in executable actions; enforcement/tests remain future work. |
| Borderline file scope | Maximum declared file count is 9 and every plan has 2 tasks. | Original warning resolved, subject to R2-02 ownership correction. |
| Probe-policy bootstrap | 07-41 specifies an Agent-owned public-only bootstrap: authorized compiled path, every answer checked, component/node/port hard denies, fixed numeric dial, no private/selfhost exemption and deny on missing trusted list. 07-42/43 keep its limited scope honest. 07-68 explicitly replaces it with full immutable policy and runs browser→PG→Agent→PG regressions, including private exceptions, selfhost, control ports, mixed answers, rebinding and old identity. | Resolved; no backwards full-policy prerequisite. |
| Pattern assignments | 07-57 action names rule.go transaction/scope/audit/task-Outbox analog; 07-60 names worker/dns.go Once/Run/current-authority analog; 07-75/76 explicitly reference RESEARCH physical backup/credential-separation patterns. Per-file classification references are now present across the slices. | Original examples resolved. |
| Browser/public security | 07-12 owns existing TLS listener integration, SPA source/serving configuration and production Compose/Dockerfile; nonce CSP, same origin, Host/Origin, TLS1.3, HSTS, no CORS relaxation, path traversal and actual browser script/frame negative tests are explicit. | Resolved in plans; no deployment or security test credited as run. |

## Requirement, decision and behavior coverage

All 27 ROADMAP IDs occur in requirements frontmatter. Concrete task coverage is present rather than relying solely on 07-84's blanket claim:

| Requirement | Concrete plans / behavior |
| --- | --- |
| FE-001–003 | 02/05/09/17–28/34–38: Chinese UI, creation entry/drawer/full-page patterns, rule list projection/order. |
| FE-004–008 | 31–43: actual runtime evidence/five states, path editing, measured segments, versioned impact preview, actionable safe errors. FE-007 delivery is present but executable tracer ownership is R2-02. |
| FE-009 | 29/30 and 39/40: loss confirmation, step-up/version checks and evidence-only port release. |
| FE-010 | 57–60: scoped announcements and authoritative notification cycles with mandatory key notifications. |
| FE-011–012 | 44/45, 51–53: degraded-state enforcement, stale values, per-source exact amounts and measured report-to-render freshness. |
| SEC-001–003 | 06/08–18/64–71 and domain negative branches: TLS/mTLS/identity/revocation, current roles/scope and backend enforcement. |
| SEC-004–006 | 08–16/55/62/70/71: real credentials/MFA, attempt and sensitive-write budgets, protected key/secret lifecycle. |
| SEC-007–010 | 74–84: actual encrypted PG base/WAL, clean-target physical recovery, independent generation/fencing, same-R/FULL, K/tails and tenant release, RPO/RTO. |
| SEC-011 | 64–68: every real production resolve/connect boundary, immutable trusted policy and dedicated probe full-policy test. |
| SEC-012–013 | 61–63/72/73: immutable scoped audits and classified hold-aware retention. |
| SUP-PAGES-001 | 01 page/role/API/task/evidence matrix, domain slices and 84 non-empty actual page coverage checks. |
| SUP-RECOVERY-001 | 74–84 six-step real recovery, source/tenant holds, physical/authority/runtime proof separation and custody. |

32 locked decisions remain represented in concrete actions and source-audit traces. Cards/list density, explicit platform tenant selection, five rule facts, final impact confirmation, TOTP/recovery codes, shared DNS state, critical notification cycles, mobile editing and light theme are retained. No decision scope is excused as “v1”/static/placeholder. Deferred dark theme, payments, team switching and unspecified cloud adapters remain excluded. Partial early tracers explicitly transfer their full behavior to later slices rather than claiming it already passed.

REVISION-MAP JSON contains 84 original task mappings; the Markdown trace retains their behavioral descriptions. The grouped capability trace remains intact, including API-only early announcement/notification slices followed by real browser delivery, original probe private-exception tests moved to 68, and full editor submission matrices assigned to 40. No intentional original behavior deletion was found. Mapping presence alone is not accepted as executable proof: R2-01 and R2-02 are the concrete failures discovered while following those mappings.

## Normal verification dimensions

| Dimension | Result |
| --- | --- |
| Task completeness | Installed verify.plan-structure was run for all 84 plans: exit 0, valid true, no structural errors/warnings, 2 tasks each. read_first/acceptance_criteria exist, but R2-01/02 fail semantic completeness despite that structural pass. |
| Dependencies | Declared graph references existing plan IDs, is acyclic and consistent with its 84-wave chain. Declared edges alone do not resolve the future prerequisites in R2-01/02. |
| Same-wave temporal coupling | One plan per wave; no same-wave pair and no coupling exemption to report. |
| Artifacts/key links | Browser→REST→service/PG and control→Agent/Engine→durable evidence connections are explicit in actions. Schema/API generation is separated from implemented route inventory; future operations remain explicitly unsupported. Preview browser wiring is R2-02. |
| Scope | 2 tasks and at most 9 declared files per plan, below file/task warning bands; actual ownership must still satisfy R2-02. No file-count exemption for generated outputs is used. |
| Must-have derivation | Domain capabilities and conservative truth/evidence constraints relate to the phase goal. Repeated generic metadata is supplemented by concrete domain actions, negative outcomes and artifacts; it does not excuse R2-01/02. |
| Architectural tiers | Matches RESEARCH responsibility map: auth/business/scope in backend, PG authoritative, Engine independent, observation over process-local interface, no backend/Hub traffic payload, physical recovery in controlled operations, generation outside restored PG. |
| Data contracts | Complete API schema generation is 07-07; complete observation/probe wire generation is 07-31; migrations and SQL generation have explicit atomic ownership. No incompatible destructive shared-data transform found. Unknown/F/FULL and PG admission stay distinct. |
| AGENTS | SQL-first, append-only migrations, exact dependency/approval/license boundaries, native evidence and architecture constraints retained. No .codex/skills or .agents/skills directories found. Historical Phase5 passed_with_scope/STATE compatibility block remains visible; no bypass is planned. |
| Review incorporation | Original PLAN-CHECK findings have executable dispositions above. No separate current REVIEWS.md input was supplied. |
| Research resolution | Six planning decisions resolved with real choices, not merely a heading rename; approvals/probes/resources/UI/runtime remain unverified. |
| Pattern compliance | Explicit per-file analog/research citations now accompany applicable source/worker/SQL/frontend/backup slices. |
| Verify format | No tree-anchored package-list grep, swallowed-error passing default or unsupported hard-coded test-count assertion found in automated commands. |

## Nyquist and supplied deterministic JSON

VALIDATION exists and remains draft with nyquist_compliant=false and wave_0_complete=false, correctly reflecting unrun implementation infrastructure. Each of 168 tasks has automated feedback; no unverified three-task window or MISSING sentinel is used. Happy/rejections branches are separately named; first-owned suites/specs/checkers and bootstrap registry provide their creation/selection contract. Future-generated public API types are not treated as implemented handlers. R2-01 remains a specific exception to otherwise explicit prerequisite readiness.

The full two supplied JSON documents were parsed and their command rows consumed without rerunning either probe:

- command paths: 168 rows, all not_applicable, severity none; readError null. The wrapper commands were **not examined by the path recognizer**, which is not evidence of runtime resolution.
- failure directions: 168/168 ok, severity none; readError null. Statements name concrete bad outcomes/nonzero/zero-test/skip/environment failures. No deterministic blocker or warning was overridden or invented.
- Long browser, network and physical recovery checks remain long-running专项 with separate timing; fast subset targets are targets, not measured latency. No tests were executed in this review.

Threat declarations include 252 unique numeric IDs plus shared T-07-SC. They remain planned mitigations requiring real negatives, not fulfilled security evidence. 53 edges remain 51 explicit planning dispositions plus E-07-52/53 unclassified and flagged; 4 descriptorless prohibitions remain flagged-unverified. Final owner/custody/mobile/manual checks cannot turn green through static plan checks.

## Per-plan scope and calibrated estimate checks

estimate-check --tokens <estimate> --calibrated was run for every plan. All returned budget=100000, over_budget=false, confidence=low, sample_count=0, calibration_factor=1. These are uncalibrated estimates, not precise observed costs. No over-budget recommendation was returned.

| Plan | Tasks | Declared files | Wave | Tokens | Budget | Confidence | Over budget |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 07-01 | 2 | 5 | 1 | 16000 | 100000 | low / 0 actuals | false |
| 07-02 | 2 | 5 | 2 | 16000 | 100000 | low / 0 actuals | false |
| 07-03 | 2 | 5 | 3 | 18000 | 100000 | low / 0 actuals | false |
| 07-04 | 2 | 5 | 4 | 16000 | 100000 | low / 0 actuals | false |
| 07-05 | 2 | 6 | 5 | 16000 | 100000 | low / 0 actuals | false |
| 07-06 | 2 | 4 | 6 | 16000 | 100000 | low / 0 actuals | false |
| 07-07 | 2 | 8 | 7 | 16000 | 100000 | low / 0 actuals | false |
| 07-08 | 2 | 9 | 8 | 16000 | 100000 | low / 0 actuals | false |
| 07-09 | 2 | 3 | 9 | 16000 | 100000 | low / 0 actuals | false |
| 07-10 | 2 | 6 | 10 | 16000 | 100000 | low / 0 actuals | false |
| 07-11 | 2 | 6 | 11 | 16000 | 100000 | low / 0 actuals | false |
| 07-12 | 2 | 9 | 12 | 16000 | 100000 | low / 0 actuals | false |
| 07-13 | 2 | 9 | 13 | 16000 | 100000 | low / 0 actuals | false |
| 07-14 | 2 | 5 | 14 | 16000 | 100000 | low / 0 actuals | false |
| 07-15 | 2 | 9 | 15 | 16000 | 100000 | low / 0 actuals | false |
| 07-16 | 2 | 5 | 16 | 16000 | 100000 | low / 0 actuals | false |
| 07-17 | 2 | 4 | 17 | 16000 | 100000 | low / 0 actuals | false |
| 07-18 | 2 | 9 | 18 | 16000 | 100000 | low / 0 actuals | false |
| 07-19 | 2 | 3 | 19 | 16000 | 100000 | low / 0 actuals | false |
| 07-20 | 2 | 3 | 20 | 16000 | 100000 | low / 0 actuals | false |
| 07-21 | 2 | 9 | 21 | 16000 | 100000 | low / 0 actuals | false |
| 07-22 | 2 | 4 | 22 | 16000 | 100000 | low / 0 actuals | false |
| 07-23 | 2 | 3 | 23 | 16000 | 100000 | low / 0 actuals | false |
| 07-24 | 2 | 9 | 24 | 16000 | 100000 | low / 0 actuals | false |
| 07-25 | 2 | 3 | 25 | 16000 | 100000 | low / 0 actuals | false |
| 07-26 | 2 | 3 | 26 | 16000 | 100000 | low / 0 actuals | false |
| 07-27 | 2 | 9 | 27 | 16000 | 100000 | low / 0 actuals | false |
| 07-28 | 2 | 4 | 28 | 16000 | 100000 | low / 0 actuals | false |
| 07-29 | 2 | 3 | 29 | 16000 | 100000 | low / 0 actuals | false |
| 07-30 | 2 | 4 | 30 | 16000 | 100000 | low / 0 actuals | false |
| 07-31 | 2 | 9 | 31 | 16000 | 100000 | low / 0 actuals | false |
| 07-32 | 2 | 4 | 32 | 16000 | 100000 | low / 0 actuals | false |
| 07-33 | 2 | 9 | 33 | 16000 | 100000 | low / 0 actuals | false |
| 07-34 | 2 | 6 | 34 | 16000 | 100000 | low / 0 actuals | false |
| 07-35 | 2 | 5 | 35 | 16000 | 100000 | low / 0 actuals | false |
| 07-36 | 2 | 4 | 36 | 16000 | 100000 | low / 0 actuals | false |
| 07-37 | 2 | 6 | 37 | 16000 | 100000 | low / 0 actuals | false |
| 07-38 | 2 | 4 | 38 | 16000 | 100000 | low / 0 actuals | false |
| 07-39 | 2 | 9 | 39 | 16000 | 100000 | low / 0 actuals | false |
| 07-40 | 2 | 4 | 40 | 16000 | 100000 | low / 0 actuals | false |
| 07-41 | 2 | 3 | 41 | 16000 | 100000 | low / 0 actuals | false |
| 07-42 | 2 | 9 | 42 | 16000 | 100000 | low / 0 actuals | false |
| 07-43 | 2 | 6 | 43 | 16000 | 100000 | low / 0 actuals | false |
| 07-44 | 2 | 6 | 44 | 16000 | 100000 | low / 0 actuals | false |
| 07-45 | 2 | 5 | 45 | 16000 | 100000 | low / 0 actuals | false |
| 07-46 | 2 | 3 | 46 | 16000 | 100000 | low / 0 actuals | false |
| 07-47 | 2 | 6 | 47 | 16000 | 100000 | low / 0 actuals | false |
| 07-48 | 2 | 3 | 48 | 16000 | 100000 | low / 0 actuals | false |
| 07-49 | 2 | 9 | 49 | 16000 | 100000 | low / 0 actuals | false |
| 07-50 | 2 | 3 | 50 | 16000 | 100000 | low / 0 actuals | false |
| 07-51 | 2 | 4 | 51 | 16000 | 100000 | low / 0 actuals | false |
| 07-52 | 2 | 9 | 52 | 16000 | 100000 | low / 0 actuals | false |
| 07-53 | 2 | 5 | 53 | 16000 | 100000 | low / 0 actuals | false |
| 07-54 | 2 | 4 | 54 | 16000 | 100000 | low / 0 actuals | false |
| 07-55 | 2 | 9 | 55 | 16000 | 100000 | low / 0 actuals | false |
| 07-56 | 2 | 4 | 56 | 16000 | 100000 | low / 0 actuals | false |
| 07-57 | 2 | 9 | 57 | 16000 | 100000 | low / 0 actuals | false |
| 07-58 | 2 | 5 | 58 | 16000 | 100000 | low / 0 actuals | false |
| 07-59 | 2 | 9 | 59 | 16000 | 100000 | low / 0 actuals | false |
| 07-60 | 2 | 6 | 60 | 16000 | 100000 | low / 0 actuals | false |
| 07-61 | 2 | 6 | 61 | 16000 | 100000 | low / 0 actuals | false |
| 07-62 | 2 | 9 | 62 | 16000 | 100000 | low / 0 actuals | false |
| 07-63 | 2 | 4 | 63 | 16000 | 100000 | low / 0 actuals | false |
| 07-64 | 2 | 5 | 64 | 16000 | 100000 | low / 0 actuals | false |
| 07-65 | 2 | 6 | 65 | 18000 | 100000 | low / 0 actuals | false |
| 07-66 | 2 | 5 | 66 | 16000 | 100000 | low / 0 actuals | false |
| 07-67 | 2 | 5 | 67 | 16000 | 100000 | low / 0 actuals | false |
| 07-68 | 2 | 6 | 68 | 16000 | 100000 | low / 0 actuals | false |
| 07-69 | 2 | 5 | 69 | 16000 | 100000 | low / 0 actuals | false |
| 07-70 | 2 | 5 | 70 | 16000 | 100000 | low / 0 actuals | false |
| 07-71 | 2 | 5 | 71 | 16000 | 100000 | low / 0 actuals | false |
| 07-72 | 2 | 9 | 72 | 16000 | 100000 | low / 0 actuals | false |
| 07-73 | 2 | 5 | 73 | 16000 | 100000 | low / 0 actuals | false |
| 07-74 | 2 | 9 | 74 | 18000 | 100000 | low / 0 actuals | false |
| 07-75 | 2 | 5 | 75 | 24000 | 100000 | low / 0 actuals | false |
| 07-76 | 2 | 5 | 76 | 24000 | 100000 | low / 0 actuals | false |
| 07-77 | 2 | 5 | 77 | 24000 | 100000 | low / 0 actuals | false |
| 07-78 | 2 | 9 | 78 | 24000 | 100000 | low / 0 actuals | false |
| 07-79 | 2 | 7 | 79 | 24000 | 100000 | low / 0 actuals | false |
| 07-80 | 2 | 9 | 80 | 24000 | 100000 | low / 0 actuals | false |
| 07-81 | 2 | 8 | 81 | 24000 | 100000 | low / 0 actuals | false |
| 07-82 | 2 | 6 | 82 | 24000 | 100000 | low / 0 actuals | false |
| 07-83 | 2 | 5 | 83 | 24000 | 100000 | low / 0 actuals | false |
| 07-84 | 2 | 8 | 84 | 24000 | 100000 | low / 0 actuals | false |

## Structured Issues

```yaml
issues:
  - plan: "07-04"
    task: 2
    dimension: "dependency_correctness"
    severity: "blocker"
    required_property: "Each task's required behavior and automated completion check use prerequisites available from earlier dependencies or created within its declared ownership."
    description: "07-04-T02 action line97 demands real page/REST/draft and dual-login browser rejection tests, but07-04 owns only dependency manifests/config/Dockerfile and runs installed-checker plus tsc. The application/index/first browser spec/check runner are produced only by dependent07-05-T01 line90."
    fix_hint: "One route is concrete installed/compile/integrity negative fixtures in04, with the full page/REST/draft rejection behavior assigned to its later producing browser slice; any route satisfying readiness and honest checks counts."
  - plan: "07-39"
    task: 1
    dimension: "key_links_planned"
    severity: "blocker"
    required_property: "The first real browser preview-confirm-execute tracer has an owned implementation of its frontend wiring or an explicitly created and verified reusable predecessor integration, with no hidden writes or backwards prerequisite."
    description: "07-39-T01 line99 requires browser DTO rendering and preview-bound submission, yet its9 owned paths include no frontend production source.07-38 line86 defers final confirmation to39/40;07-40 owns rule-editor.tsx but depends on39. No predecessor explicitly implements the exact generic rule-preview confirmation path needed for39 to pass without frontend writes."
    fix_hint: "One route is a bounded coherent slice owning both browser wiring and its tracer; another is a proven reusable predecessor, or an honestly API-only earlier slice with the full browser tracer moved to its source owner. Preserve all behavior, actual output ownership and scope bounds."
```

## Recommendation

Two blocking properties require targeted revision. The issue count decreased from seven to two; iteration 2/3 has not exhausted the revision cap and is not a stall. A full plan rewrite is unnecessary: correct the finite 07-04 and 07-38/39/40 task boundaries and their matching ownership/validation/mapping records, then recheck the resulting concrete plans. This review does not authorize dropping locked behavior, hiding outputs, weakening tests, or converting pending approvals/runtime evidence to passed.
