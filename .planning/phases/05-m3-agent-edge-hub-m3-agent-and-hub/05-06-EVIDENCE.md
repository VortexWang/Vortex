# 05-06 incremental evidence — Task 1 only

Status: **awaiting owner decision**, not a completed plan. Exact lifecycle proposal SHA-256: `fc67a1ec0b2036530d836f0d4e4b59694f21498230d45128b1f4a96dcce36e3a`. Candidate manifest: `37627f6985aaa37df18c1f1f0734d7e4293a73066faa30cfa5b43c792943c346`. No approved contract/version/governance/CHANGELOG bytes changed. Existing dirty 05-03/Phase4/wave2 files were preserved and are outside this task.

## Executed checks

- `PATH=/tmp/vortex-protoc-36.2/bin:$PATH python3 -m unittest discover -s tests/phase1 -p test_lifecycle_candidates.py -v`: **13 tests passed**. Positive projections, genuine protobuf binary encode/decode, existing tag/type compatibility, request digest, overflow/mutated identity, share-lineage/anchor, policy boundaries, legacy authorization branch, blocked Hub and restriction/handoff cases. Test oracles are contract fixtures, not production validators or durable runtime behavior. No new Python dependency was installed.
- `PATH=/tmp/vortex-protoc-36.2/bin:$PATH sh tools/phase5/contracts_check.sh`: **passed** with protoc36.2, protoc-gen-go1.36.12, protoc-gen-go-grpc1.6.2. Existing generated files reproducible, frozen control/auth checks, mapping, five tool unit tests and change-control checks pass unchanged.
- `python3 -m unittest discover -s tests/phase1 -p test_contracts.py`: **4 passed**.
- `python3 tools/phase1/contracts.py check-frozen --contract-id vortex.runtime-config`: **passed**.
- Generated candidate protobuf/grpc Go into a temporary directory with the pinned generators, compiled using Go1.27.3 and current locked root module. **Compile succeeded**; Go reports `[no test files]`, which is not counted as an execution test. No generated candidate file was copied into the frozen directory.
- Current baseline input hashes rechecked against manifest, all unchanged. The manifest also records reachable source baseline7fc7dba for archival compatibility tests after eventual approved freeze.

Machine details, commands' raw output hashes and limitations: `artifacts/phase5/05-06-candidate-review/report.json`; raw logs in the same directory.

## Limits and next checkpoint

These are local macOS candidate checks on the working-tree candidate bytes, not a clean same-SHA Docker/Actions pair. Dual-host results remain unrun for this increment and mandatory before completing 05-06. No runtime lifecycle, financial CAS, actual certificate-rotation or process-recovery gap is marked closed by these fixtures. G05-01/02/03/05 stay open; old UAT does not approve new behavior.

Task 2 requires owner approval of exact proposal hash and versions control1.3.0, authorization1.2.0 and runtime1.1.0. Only then may Task3 copy candidate bytes, bind normative companion hashes in governance, regenerate production Go, update versions/map/CHANGELOG and prove final compatibility. Do not create a completed 05-06-SUMMARY before that work and required evidence.
