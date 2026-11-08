# 06-06 owner checkpoint — CR-008

Implementation and candidate runtime validation complete; awaiting exact owner approval. Phase6 stays8/11. No06-06 SUMMARY, contract freeze or new approval record has been created.

- Candidate: `docs/changes/CR-008-limits-runtime.json`
- Candidate SHA-256: `4e29497f82808b9a8693030ef400dae3a2bc42cc27cf8a39f24bdcf8605074bd`
- Readable review: `docs/changes/CR-008-limits-runtime.md`
- Proposed versions: database/API2.3.0, control1.6.0; other frozen contracts unchanged.
- Exact tested source: `cf92b7e24b6c557a5d14a18b0549a67eac349eb4`
- Raw clean Docker proof: `artifacts/phase6/06-06/task3-unified-acceptance/cf92b7e2/docker/`
- Semantic evidence checker: `tools/phase6/limits_evidence_check.py`; successful result and five malformed evidence rejections are retained beside the raw run.

Complete native backend race, dependency/license checks, two independent sqlc/protobuf generations, historical exact approvals, legacy wire and OpenAPI validation pass. The clean Docker run passes backend normal/race, Agent/Store/Hub/contracts/Engine normal/race, real bounded full-disk cases, ledger/share fault injection and Node/Backend/Hub Linux amd64/arm64 builds. Source is clean before/after. Docker Linux arm64 execution is not nativeamd64 or same-SHA Actions/Ubuntu qualification.

Earlier extra reconnect observation remains unexplained and retained as failed development evidence. Later isolated3x, affected suites and this clean normal/race/fault run keep and pass the strict assertion. Bounded Engine fixture, physical power-loss and complete-model limits remain explicit in LIMITS.md; no broader system acceptance is implied.

Gate source: `docs/governance/contract-review.yaml` requires owner approval, SemVer, requirements-map and changelog review for normative changes. CR-007 approval does not cover CR-008. Await an explicit owner response binding this candidate before writing CR-008-APPROVAL, freezing its contracts, completing06-06 or executing dependent06-07. Final paired acceptance stays06-11.
