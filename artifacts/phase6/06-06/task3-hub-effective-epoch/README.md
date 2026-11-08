# 06-06 Task3: admitted effective-epoch issue / runtime / seal consumers

Development evidence on base `dee12347`. This is not a clean-source Docker /
GitHub Actions pair, full06-06 acceptance or native amd64 execution. Phase6 stays
8/11;06-06 Task3 and CR-008 remain incomplete/unapproved.

The backend derives an effective allocation from immutable original bytes and
every adjacent committed admission, approval, manifest and authenticated FULL
Node fence/root receipt. Original allocation/delegation hashes, parent, sources,
generation, limits and all original deadlines must match at each step. The
current bound boot/epoch and exact latest three handoff proofs gate new issues,
runtime leases and empty heartbeats. Old exact receipts replay before current
authority/time gates; a successor boot cannot relabel them. Global Hub then
tenant locking serializes these consumers with admission and each other.

Actual Hub SQLite journals carry37 old and10 new raw bytes against the same103
raw parent. No new financial reservation or capacity appears. The complete
two-epoch seal returns only56 never-issued raw;47 issued raw and unknown child
debt stay held.8 concurrent issue/seal callers remain idempotent. Issue, runtime
and seal audit failures roll back receipt/task/operation/Outbox/financial writes.
Fresh runtime records after sealing deny; exact historical replay still works.

Proof negatives cover missing proofs, changed credit/nonfinancial R, manifest,
transfer, root revision, source, deadline, old boot and aggregate raw overflow.
Approval/retention without admission cannot register new-epoch rights. A Go
overlay deliberately bypasses only the issue handoff-equality gate: the missing
proof test then fails by detecting acceptance. Source hashes before/after agree;
normal source passes the same test.

The actual TLS test uses independent backend/Hub roots and the production
BackendClient/HubAdapter funded stream. The fixture explicitly completes the
finite local ACK barrier beforehand; production admission/ACK transport is still
pending. Real new-epoch issue/lease/seal delivery survives FULL seal receipt loss,
PG ACK loss and two reconnects without creating another partition or freeing
issued debt. This test advances a synthetic issuer clock before connecting;
it does not claim a real60-second wait (that separate existing test remains in
the complete backend suite).

Evidence and corrections:

- `baseline-consumers-native.log`, `focused-native.log`: initial passing checks.
- `actual-mtls-native.log`: failed build, an unused test import; corrected.
- `actual-mtls-native-corrected.log` and `actual-mtls-native-diagnosis.log`:
  failed because a fixed test clock repeated the durable heartbeat at reconnect.
  A monotonic real-time test clock corrects the fixture; production heartbeat
  guards were not changed.
- `focused-native-final.log` / `focused-linux-final.log`: new effective tests,
  including actual role-TLS receipt/ACK loss and reconnect, pass.
- `backend-native.log` / `backend-linux.log`: initial complete suites fail on the
  historical cold-recovery test's stable `ErrScope` expectation. The receipt was
  denied but returned conflict after moving the exact replay gate. Restored
  `ErrScope` for old boot/Hub mismatch. Final suites include the corrected gate
  and the newly added transport test.
- `backend-native-final.log` / `backend-linux-final.log`: final complete backend
  race suites; their final process results are recorded in the manifest.
- `root-native.log` / `root-linux.log`: complete Agent, Hub, contracts and Node
  CLI race packages after the additive validated-inspection projection change.
  Agent/store production is unchanged and its previous complete proof remains
  separate; this unit does not claim that Store tests were rerun.
- `red-without-handoff-gate.log` / `red-overlay.json`: expected RED under temporary
  overlay, no working source edit. `green-gates-final.log`: normal proof gate
  and historical cold-boot scope-error compatibility pass afterward.
- `sqlc-repro.json`: two isolated23-output generations match each other and
  the worktree. No schema/query/generated/protobuf source changes in this unit.
- `cross-builds-final.json`: final Backend/Hub/Node cross-builds for Linux amd64
  and arm64. `cross-builds.json` precedes the stable error-code correction.

Remaining: production admission/ACK and explicit orchestration; generation-level
new-epoch renewal/closure; repeated PG recovery producers; full producer and
pending-rule convergence; exact CR-008 review and06-11 same-SHA acceptance.
