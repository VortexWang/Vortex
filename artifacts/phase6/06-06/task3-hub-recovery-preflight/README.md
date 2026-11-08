# Hub same-store recovery preflight — development evidence

Production Hub opens an independent role-mTLS `hub-sr-preflight-v1` stream. Only an existing explicit approval matching its actual boot can supply the immutable FULL S/R command. A normal current boot receives an advisory and opens a separately gated ordinary funded stream. Legacy/combined capabilities cannot acquire recovery commands.

The client uploads the complete retained prefix through S+1 in bounded 32-row pages, including previously confirmed rows. Typed scope-bound record and manifest retentions do not confirm a ledger, bind a new incarnation, settle funds or enter online mode. The actual mTLS test uses 41 old issues plus handoff (42 records, 77 raw bytes), loses one committed record retention and one committed manifest retention, and reconnects twice. Exact replay retains all evidence with confirmed=0 and pending=42; fresh issue and new-boot Bind remain denied. An empty Hub store cannot assert continuity. A separate ordinary-funded regression proves preflight advisory compatibility with allocation/issue receipt loss and reconnect.

Final role-separated focused tests use independent Hub/Node CA roots. A Node-role certificate claiming the same Hub UUID fails TLS. Immutable approval lookup also covers authenticated scope, old/wrong boot/backend and audit rollback; local command negatives cover complete session/identity/digest/unknown-field binding before FULL.

Validation:

- Full native backend race: service197.353s; full native root components: Agent56.599s, Store67.391s, Hub11.013s, contracts2.048s, NodeCLI3.471s.
- Serial Linux root race: Agent94.283s, Store133.200s, Hub18.023s, contracts1.018s, NodeCLI2.348s; full Linux backend race: service205.480s.
- Full suites include all final production changes. Final test-only independent role-root/forged Node identity refinement was added afterward: focused native5.524s and Linux5.436s both pass. Full suites are not represented as having run the final test variant.
- Two isolated pinned generations match 23 sqlc and two protobuf outputs. Six Backend/Hub/Node Linux amd64/arm64 builds pass; these do not prove native amd64 execution.
- Current candidate8 native/Linux node_credit_fence databases were freshly migrated in the preceding unit; this unit adds no DDL. Source whitespace checks pass. Raw Docker logs retain original whitespace.

These are working-tree development results, not clean-source acceptance or GitHub Actions evidence. Phase6 remains8/11;06-06 Task3 is incomplete; no06-06 SUMMARY. CR-008 is unapproved. Current-state admission, independent Node successor roots, effective epoch consumers and full producer convergence remain required. Prior approved protocol snapshots are unchanged.
