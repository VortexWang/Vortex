# 06-06 Task3: cold closure of transferred generations

Development checkout on basea75bae91. Phase6 remains8/11;06-06 incomplete and
CR-008 unprepared/unapproved. No clean-source same-SHA Docker/Actions pair.

The cold proof consumer is independent of the live same-store chain, so a fresh
cold boot need not fabricate an S/R admission. Canonical immutable payload/hash,
request columns and both original/current incarnation bindings are checked.
The exact original share must appear once, its deadlines must be covered and
trusted current lower time bound must be at/after the complete old hard deadline.
The producer remains the existing explicit platform-admin all-tenant recovery.

For transferred generations, the original renewal-authority row and its first
admission closure stay byte-for-byte unchanged. Preparing closure retires only
unacknowledged live closure tasks/operations/Outbox as failed in its existing audit
transaction, without manufacturing Hub receipts. QueueHandoff independently
consumes the proof before binding CAS. Cold closure does not enable renewal.

A real PG/Hub SQLite admitted7->8 fixture, explicit cold8->9 proof and actual
Node SQLite FULL CAS keep original funds and unknown occupation held. Eight
concurrent closure calls and audit failure rollback pass. Missing/changed/duplicate
share, incorrect boot columns, bad digest, premature proof/current lower bound
are rejected without authoritative writes. Actual CAS releases600000->300000
capacity only; original closure and financial rows remain unchanged. Cold proof
and command replay are stable. This is a service/store test, not a new network
recovery orchestration acceptance test; existing role-mTLS regressions run in the
complete backend suites.

Retained failed attempt: focused-complete-native.log changed the first proof
share (connections) while testing the rate share. The fixture now locates rate
explicitly. Unrelated share is outside that individual consumer's scope; the
trusted immutable producer covers all original rights. Production guards were
not loosened. focused-native.log and focused-corrected-native.log pass.

Expected RED uses a Go overlay omitting the new cold proof consumption before
CAS: the new test must fail because no S/R chain enters the cold epoch. Source
before/after SHA is identical; normal focused GREEN passes. Native/Linux final
suite and four Backend/Hub cross-build results are recorded separately.

No SQL/protobuf generation input, migration, root Agent/Hub source, approved
snapshot, public API or dependency changed. Root and generation evidence from
prior commits is inherited unchanged, not rerun or relabeled. Linux container
execution is arm64; cross-builds do not prove native amd64 execution. Final
same-source Docker/Actions is06-11. Remaining06-06: explicit platform orchestration,
effective funding/refill, RuntimeServer/pending-rule convergence, complete producer
fault/clean-source checks and concrete CR-008 review.06-07/06-11 remain pending.

Final complete backend native race passes(service287.287s); Linux arm64 race passes(service303.517s). Four Backend/Hub cross-builds pass.
