# 05-06 approved lifecycle contract evidence

Status: **plan complete; runtime gaps remain open**. Implementation SHA `8ca29f833e2c7bccb2eba3e668d1511c25d4f726`.

## Approval and exact bytes

The owner answered “批准” to proposal SHA-256 `fc67a1ec0b2036530d836f0d4e4b59694f21498230d45128b1f4a96dcce36e3a`, manifest `37627f6985aaa37df18c1f1f0734d7e4293a73066faa30cfa5b43c792943c346`. Formal files are byte-for-byte copies of the approved candidates: control 1.3.0, authorization 1.2.0 and runtime 1.1.0. Governance records the approval and hashes of the normative lifecycle schema, semantics and certificate identity profile. Historical proposal/manifest retain their original review bytes; current approval is in governance.

Task 1 candidate commit: `cf2ef60872c02c97aae770762cd6b04ad31bb79c`. Task 2 is the owner's exact approval. Task 3 implementation: `8ca29f8`.

## Executed verification

- Local macOS arm64 Go1.27.3: pinned protoc36.2 contract gate, `go test ./contracts/...`, root and independent Engine `go test ./...`: passed. Contract gate runs 7 tool tests, 4 existing contract tests and 13 candidate test methods. Generated Go is reproducible with protoc-gen-go1.36.12 / protoc-gen-go-grpc1.6.2.
- Local Docker Linux arm64: clean clone of implementation SHA, `make phase5-check`: passed. Pinned Go1.27.3 image, real isolated 4MiB full-disk fixture, root/Engine tests, Engine race and recovery faults all passed.
- GitHub Actions Ubuntu24.04 amd64: same SHA, full gate passed; [run42150094526](https://github.com/VortexWang/Vortex/actions/runs/42150094526). Workflow also passed contract, Wave1/Wave2 and incremental authorization checks.
- Independently reran `tools/phase5/audit_recovery.py` against each host's downloaded/raw ledger snapshots in the pinned Linux container: passed, 5 snapshots and 10 required fault cases per host. Hashes, integrity, source conservation and unresolved unknowns checked. The macOS system Python/SQLite3.51.0 auditor could not open a WAL snapshot; its audit is not counted as passed. Linux re-audits passed.

Raw logs, SQLite gzip snapshots, host reports, pair metadata and SHA256 file inventory: `artifacts/phase5/check/8ca29f833e2c7bccb2eba3e668d1511c25d4f726/`. Actions raw files were copied from the artifact's current root only; historical nested evidence was excluded. Earlier candidate-only evidence remains `artifacts/phase5/05-06-candidate-review/` and is not substituted for the paired result.

## Coverage and boundaries

| Deliverable | Actual check | Result |
|---|---|---|
| Exact schemas and additive compatibility | `tests/phase1/test_lifecycle_candidates.py` binary/projection fixtures, archived old input baseline | pass, finite contract fixtures |
| Approved production wire round trip | `contracts/lifecycle_test.go:TestLifecycleGeneratedWireRoundTrips` | pass, wire only |
| Normative companion freeze | `tools/phase1/contracts.py:check_frozen`, `changed_contract_ids`; missing/duplicate/drift and companion-only change tests | pass |
| Frozen versions/map/governance/generation | `tools/phase5/contracts_check.sh` on both hosts | pass |
| Existing debt/recovery regressions | complete Phase5 gate plus independent audit | pass within protocol-test boundary |

T-05-09/10 approval, typed bounds, replay/compatibility mitigations are checked at the contract prerequisite boundary. No new production authority consumer or financial transfer is introduced here. Current Agent authorization consumer remains on legacy1.1 until dependent implementation; no stripping/downgrade of new restrictions is authorized. G05-01/02/03/05, service lifecycle, actual certificate rotation, backend catchup and share/policy runtime behavior remain open for 05-07..14. Production backend authority stays Phase6; Phase4 qualification remains independent. No requirement-wide completion or new UAT pass is inferred.

## Plan deviations

Used existing `contracts/CHANGELOG.md` (the plan names nonexistent root CHANGELOG.md). Added Go wire tests and normative companion hash tests to prevent an unversioned sidecar from bypassing the approved freeze. No dependency was added. Unrelated 05-03/Phase4/wave2 changes were preserved.
