# Direct Node successor roots — development evidence

Explicit platform preparation derives one immutable next source provision for an approved S/R allocation only after a verified PG manifest and authenticated Node FULL inventory fence. Hub/Node/tenant locks stabilize current boot, authority, seals and exact child inventory. All three roots advance only revision and epoch; original source, share generation, Node/rule/path, limits and deadlines remain identical. Original allocation, provision, funds, share/capacity and old unknown liabilities remain unchanged.

A production worker delivers committed bytes over the existing independently UUID-pinned direct-backend source capability. A distinct pending recovery request selects the recovery receipt consumer; it cannot complete from an unrelated ordinary source receipt. Command/task/Outbox/audit and receipt/task completion/audit each commit atomically. Root receipts do not bind the Hub, confirm its ledger, create executable grants or release financial/capacity occupation.

Real PostgreSQL tests cover missing FULL receipt, scope refusal, command and receipt audit rollback, eight concurrent command/receipt callers, unchanged source/limit/deadline fields, exact expired replay, changed Node boot/authority and expired first preparation. Actual Node mTLS installs original37-byte credit, records its fence, delivers next roots, loses the first FULL receipt and reconnects for exact replay. Local old-credit conservation and PG financial/share histories remain unchanged; target Hub Bind still denies. Legacy unconfigured direct clients cannot receive the provision.

Source FULL replay now precedes clock observation. A fresh first installation may intersect authenticated issued time with an already trusted conservative interval; stale boot bootstrap and continuity checks remain mandatory. The Agent regression covers trusted lower-bound lag, clock-free exact replay and stale first-install refusal.

Evidence boundaries:

- `focused-native.log`/`actual-mtls-native.log` passed before the additional local-balance assertion/clock regression. `focused-corrected-native.log` passes the final local-balance assertions.
- Initial `backend-native-race.log` and `linux-race.log` fail compilation because the test used credit.ID instead of AuthorizationID; corrected full runs retain separate logs. The initial Linux command stopped before root/full backend execution. No failed run is counted as passed.
- `agent-replay-corrected-native.log` fails because the test clock was fixed before the actual received timestamp; the final test uses a progressing clock and passes in `agent-replay-final-native.log`. `root-native-race.log` predates the trusted lower-bound refinement; `root-native-race-final.log` covers final production source.
- Full final native backend race: service222.972s. Full final native root race: Agent62.244s, Store81.187s, Hub12.134s, contracts2.120s, NodeCLI4.625s.
- Full final Linux root race: Agent106.805s, Store149.264s, Hub20.606s, contracts1.026s, NodeCLI2.534s; full final Linux backend race: service209.360s. Linux tests run in the local aarch64 VM with real PG/Valkey, sequential root/backend suites.
- Two isolated sqlc1.31.1 generations reproduce all23 output files. Protobuf is unchanged. Six final Backend/Hub/Node Linux amd64/arm64 cross-builds pass; initial cross-builds predate the clock refinement and remain separate.
- Fresh candidate8 native/Linux recovery_sources databases contain the two new immutable tables. Previously applied candidate8 databases were not modified. Raw logs are retained verbatim; source whitespace checks pass.

These are working-tree development checks, not clean-source/Actions or native amd64 execution evidence. Phase6 stays8/11;06-06 Task3 remains incomplete; no06-06 SUMMARY or CR-008 approval. Next: current-state all-tenant admission, explicit separate admission/ACK, effective epoch consumers, repeated recovery and complete producer/pending-rule convergence.
