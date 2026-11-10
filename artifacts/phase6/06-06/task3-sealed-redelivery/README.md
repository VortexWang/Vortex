# 06-06 Task3 sealed redelivery and empty-state boot guard

Candidate development evidence; no completed06-06 or final Phase6 acceptance.
Go1.27.3 native Darwin arm64 and Docker Linux arm64, isolated PostgreSQL18.6 DBs.

Review found a real negative case: PG accepted a zero-child FULL seal and returned
the never-issued parent funds, but its earlier installation receipt was absent.
The old pending-allocation query still selected that allocation, and an empty
Hub DB could otherwise report blocked epoch0 without installation evidence.

- `red.log` reproduces the defect: sealed allocation remains deliverable after
  returning the parent, without a PG installation ACK.
- `focused-race.log`: sealed allocations are excluded from redelivery; the seal
  itself fences empty-state boot reuse. Actual TLS1.3 empty-store reconnect is
  denied before any allocation install and remains epoch0; ordinary existing FULL
  journal lost-receipt/ACK replay and independent generation tests still pass.
- `issuer-evidence-race.log`: accepted issue mapping alone, without install receipt,
  also counts as durable issuer evidence; actual empty-boot seal negative passes.
- `backend-all-race.log`: complete native backend race suite.
- `linux-race-build.log`: Linux service/control/worker/timeauthority race and backend
  Linux amd64/arm64 builds. Cross-build is not native amd64 execution/performance.
- `generators.json`: two independent clean sqlc v1.31.1 runs match all18 outputs.

`sqlc-initial-failure.txt` records the captured ambiguous hub_id inference error;
`compile-*-sqlc-interface-failed.log` retain sqlc's initial interface{} inference
for a multi-EXISTS boolean expression. Explicit aliases and boolean cast fix the
query before the passing checks; no fallback or ignored generation failure remains.

Existing durable state can replay; empty state with prior installation, issue or
seal evidence cannot bootstrap old authority. This is a denial guard, not explicit
Hub/Node boot recovery or child-stop proof. Missing final child/unknown evidence
still retains all issued holds; sustained automatic refill remains incomplete.
The adjacent clean061d3d8a manifest covers only the preceding seal commit through
the existing06-05 entry, excluding later binding/redelivery changes, complete06-06
producer and Actions pairing. CR-007 is approved; CR-008 remains unprepared and
unapproved. Phase6 stays8/11 and no06-06 SUMMARY is produced.
