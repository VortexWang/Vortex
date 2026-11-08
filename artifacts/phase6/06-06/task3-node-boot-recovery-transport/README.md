# Node cold-recovery direct transport — development evidence

Base source: eb5badcd. No further candidate8 schema changes; this unit adds a pending-command query, independent node-boot-recovery-v1 capability and additive candidate NodeMessage23/ControlMessage22 envelopes. Approved contract snapshots stay immutable and CR-008 remains unapproved.

Only a UUID-pinned direct-backend Agent advertises the capability; regional delivery is rejected and other existing/future capabilities imply no recovery authority. Command identity, current boot, canonical bytes, SHA and unknown protobuf fields are checked before SQLite mutation. First retirement requires trusted bounds; an untrusted clock observes the authenticated response under the existing continuity/freshness rules. Exact FULL replay is attempted before bootstrap, so expired/stale clock cannot prevent immutable local replay or lower subsequent CAS heads. The production worker only delivers existing explicit platform approvals and never changes them. Server correlation checks identity/boot/sequence/current request and payload SHA; the PG service performs exact command/receipt verification and atomic audited retirement. Sending alone or losing a receipt leaves the original PG occupation unchanged.

Evidence:
- root-compile.log and backend-compile.log: initial component compilation.
- actual-mtls-native.log: negative fixture initially configured ControlOnly without a pinned backend and correctly failed before connection. No guard was relaxed.
- actual-mtls-native-corrected.log: real role-separated TLS1.3, unpinned capability denial, injected loss after SQLite FULL, exact reconnect replay into PG, unchanged original held capacity and zero active capacity. Includes PG proof/capability regression tests. Compiled before the final local replay-before-clock hardening.
- focused-agent-native.log: final replay-before-clock implementation, wrong backend/Node/boot/digest/unknown-protobuf rejection and stale first-install refusal.
- root-components-native-race.log: complete native Agent/Store/Hub/contracts/Node CLI race on final source.
- backend-native-race.log: complete native backend race on final source, including real cold-recovery transport.
- linux-components-backend-build.log: Linux arm64 component/CLI race, service/control/worker/timeauthority race and Backend/Hub/Node linux amd64+arm64 cross-builds. Cross-builds do not prove native amd64 execution.
- generators-reproducibility.json: two independent temporary generations match22sqlc and2protobuf outputs.

These are development results. No clean committed-source Docker/Actions pairing or full06-06 acceptance is claimed. Same-store Hub S/R, original pending-rule convergence, full06-06 producer and concrete CR-008 review remain. Phase6 stays8/11; no06-06 SUMMARY.

Final results: complete native backend race passed(service188.325s); native Agent62.689s/Store64.467s/Hub11.989s/contracts3.050s/CLI3.571s all passed. Linux arm64 Agent108.095s/Store122.539s/Hub22.089s/contracts1.027s/CLI2.896s and backend service171.635s/control1.417s/clock1.007s passed, followed by all six backend/Hub/Node linux amd64+arm64 cross-builds (command exit0). After these runs only an explanatory clock comment/doc wording changed, with no behavior change.
