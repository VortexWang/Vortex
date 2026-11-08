# Effective funded renewal trigger — development evidence

RED reproduced an active, low-water original credit family losing its automatic
renewal candidate after valid adjacent S/R. The old implementation required both
the original share revision and original signer epoch to remain current.

The observer now derives the effective epoch only through retained source-owner
handoffs and exact flat-root revisions. Original source/parent/Node/boot, limits,
generation, policy and time bounds remain fixed. The independently installed
latest backend anchor and current share head must match; both selected grant and
original source owner remain active. Current path/fencing floors still apply.
The change does not authorize new sessions on an old revision or spend R.

The FULL trigger journal rechecks the same evidence and current fence, closing
the candidate-to-journal race. Tests cover stop/root/epoch changes between these
steps, stopped/missing-handoff/changed-limit/old-path/wrong-fence/old-boot/untrusted
negatives, FULL rollback,8 concurrent exact replays and reopen. Original pending
200-byte hold and balance remain unchanged. Existing journal and refusal tests
also pass. The genuine failing RED remains in red-store.log.

Production RuntimeServer/Agent plus independent Hub/backend mTLS/PG usage ACK
also produce one epoch8 candidate after actual recovery installation and persist
its trigger; stopped sources produce none. This is candidate/journal evidence,
not a claim that this test sends and installs a new renewal over that connection.
The inherited complete original-credit final/refill assertions still run.

Native evidence is Darwin arm64, Linux evidence is Docker arm64 development
checkout. Six Linux amd64/arm64 builds are compilation evidence. SQL/protobuf,
generators, dependencies and approval/review bytes are unchanged; lifecycle.md
is candidate documentation. Complete multi-rule Engine scheduler/renewal delivery,
pending deployment convergence, other recovery/revocation orderings and full
06-06 verification remain. No clean-source same-SHA Docker/Actions pair or native
amd64 execution is claimed. Phase6 stays8/11;CR-008 unprepared/unapproved.
