# Phase 5 gap planning execution contract

Invocation: `$gsd-plan-phase 5 --gaps`. Phase is Executed/gaps_found, not closed. Existing plans and completed UAT are retained; only new 05-06..05-14 are appended. G05-04 remains resolved by f6a5891, not reopened. Work and semantic checking are inline per owner request: there is no independent subagent review claim.

## Scope and order

| Plan | Wave | Gap | Delivered boundary |
|---|---|---|---|
| 05-06 | 5 | G05-01/03/05 prerequisite | Exact candidate schemas, executable round-trip fixtures, owner approval, then freeze |
| 05-07 | 6 | G05-01 | Real node regional failover/current-session fencing |
| 05-08 | 7 | G05-01 | Probe/report scheduler, durable remote renewal, safe retention |
| 05-09 | 8 | G05-03 | Shared control/VLink identity publication and revocation |
| 05-10 | 9 | G05-02 | Commands, actual admission/drain and deployment artifacts |
| 05-11 | 10 | G05-02 | Signed service replacement and financial-safe rollback |
| 05-12 | 11 | G05-05 | Durable recovering state and caught-up checkpoint |
| 05-13 | 12 | G05-05 | Complete share chain/CAS and immutable policy cutover on real traffic |
| 05-14 | 13 | all open gaps | Same-SHA process evidence, review/security/validation and renewed UAT |

This conservative dependency chain avoids overlapping edits to main, ControlClient, store, Engine runtime and protocol fixture. All dependencies on existing plans refer to completed summaries. No production backend or Phase 4 performance closure is added. Phase 6 ownership is explicit for production CA/token/delegation/policy allocation/backend transport. Phase 5 must still implement actual Agent/Hub adapters and runtime behaviors against authenticated protocol fixtures; D-19 does not permit model-only substitutes.

## Gates and defaults

05-06 creates reviewable exact bytes before asking approval; plan authorization is not contract approval. Current control 1.2.0, authorization 1.1.0 and runtime 1.0.0 remain frozen until owner-approved hash/version update. The contract prerequisite uses executable candidate fixtures, while its first real running tracer necessarily follows approval in 05-07. All subsequent plans lead with actual runtime slices.

Local additive SQLite migrations and in-process interface changes are coordinated/costly; destructive migration and authoritative DB rollback are prohibited. New on-wire/public schema semantics are one-way and have an explicit approval checkpoint before applying. No new dependency versions are selected by this plan run. Numeric values in plans are configurable development candidates from current reference semantics, not a new frozen V1.11 acceptance baseline.

Security plan registers reserve T-05-09..26 after the existing audited register T-05-01..08. They are planned threats, not already verified closed. Plan 05-14 re-runs the security audit and appends verified results. Nyquist remains partial until actual new behaviors and same-source checks run. Preserve the historical completed UAT; new integration behavior receives renewed acceptance.

## Spec-less edge assumptions (no silent drops)

The phase has no SPEC. Deterministic fallback ran over all 34 requirement IDs and their referenced source semantics, explicitly classifying stateful I/O. It surfaced 68 proposed edges (idempotency and concurrency per ID); all 68 remain unresolved/flagged assumptions in 05-GAP-EDGE-PROBE.json. These source semantics are reference-only, so the planner does not auto-upgrade them into a new frozen spec. Every gap plan carries the relevant rows as explicit must-have backstops; at verification, missing evidence must remain human_needed/gaps_found, never silently pass. Specific runtime/negative acceptance criteria are already explicit in the tasks. Existing delivered behaviors remain protected by the final all-phase regression.

Two bespoke prohibition recalls survive precision filtering: unknown liabilities cannot be released by lifecycle changes, and test fixtures cannot be represented as production authority/qualification. Their PLAN must_haves.prohibitions are emitted via the canonical projectProhibitions serializer, descriptor-less and flagged unverified; final verifier must supply actual evidence. Generic credential/path security concerns are referred to secure-phase threat models, not minted as extra bespoke prohibitions.

## Preflight observations

UI gate: frontend=false. API gate: no external API integration; internal NodeControl is not an LLM/AI agent. Assumption-delta detector: detected=false. Context drift: no stale artifacts reported. Codebase-map drift: skipped because no STRUCTURE.md; actual source scan and PATTERNS supply mapping. Existing dirty 05-03/Phase4/wave2 changes are outside this plan and will not be committed. Independent checker isolation is unavailable under the owner's no-subagent instruction, so check results are explicitly inline plus deterministic validation, with no claim of independent review.
