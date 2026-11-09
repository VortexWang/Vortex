# Node recovery credit fence: PG and direct mTLS candidate

The Node FULL inventory primitive is now reachable only over the independently
negotiated `node-credit-recovery-v1` direct backend channel. The PG producer
derives exact old child IDs/amounts and original raw/issued/R from a verified
immutable manifest, original allocation and backend-owned root bundle. Immutable
command/operation/task/Outbox/audit commit together before the production worker
sends it. Canonical current-boot mTLS receipts complete only that command.

Evidence proves audit rollback,8 concurrent command/receipt replays, no command
before a verified manifest, configured backend/Node/boot/digest/unknown-field
isolation, exact FULL replay before clock bootstrap, old-capability refusal,
lost FULL receipt/reconnect, actual37-byte original installed child and unchanged
financial/share rows. The target Hub remains unbound and BindHubSession denies
it after the Node receipt. Expected-but-not-installed old children remain
occupied and cannot inflate R.

- `backend-native-race.log`: full backend race passed(service184.812s).
- `root-native-race.log`: full root components passed(Agent53.936s, Store66.969s).
  The new Agent identity/replay test was subsequently added and passed in
  `focused-agent-native.log`; it is included in the final Linux root run.
- `linux-race.log`: root components all passed(Agent97.768s, Store136.144s,
  Hub19.209s, contracts1.025s, CLI2.755s). The following backend stage failed to
  compile a newly added test because inspection-only records were supplied
  directly to the Store API. This combined command exits1; it is not a full pass.
- `focused-final-native.log` retains the same compile failure. Replacing that
  test fixture input with the formal InspectIssuedCreditGrant parser fixed it.
  `focused-final-native-corrected.log` and `focused-final-all-native.log` pass,
  including the actual installed original child and unchanged Hub admission.
- `backend-linux-race-corrected.log`: complete backend race passed(service199.558s)
  after the fixture correction, on the new candidate8 Linux database.
- The final production edit additionally clamps first-install fence time to the
  original allocation expiry/hard deadline. `final-boundaries-native.log` and
  `final-boundaries-linux.log` pass all new service/control race tests after that
  edit. Full backend runs above precede that final tightening; do not claim an
  additional complete final-source backend run from these focused results.
- `cross-builds-final.json`: final backend/Hub/Node builds for Linux amd64+arm64.
  This is not native amd64 runtime or performance evidence.
- `generators-repro.json`: two independent pinned generations match all23 sqlc
  and2 protobuf outputs. protoc initially resolved an unintended grpc plugin
  through PATH; explicit pinned PATH restored v1.6.2 and removed grpc file drift.
- `environment.txt`: actual tool and Docker versions/architecture.

Fresh databases: vortex_node_credit_fence_20261108(native) and
vortex_node_credit_fence_linux_20261108(Linux), migrated serially using the amended
candidate8. The Linux host is aarch64 under the local Docker VM. All evidence is
development worktree evidence, not clean-source or GitHub Actions acceptance.

This does not activate Hub S/R, ACK the retained prefix, deliver successor Node
roots, settle usage or release money/capacity. Independent recovery preflight,
current-state admission, effective new-epoch issue/lease/seal consumers, original
pending-rule convergence and complete06-06 clean-source producer remain. No
approved snapshot is modified, CR-008 remains unapproved and Phase6 stays8/11.
