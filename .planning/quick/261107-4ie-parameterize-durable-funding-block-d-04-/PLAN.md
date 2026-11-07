---
quick_id: 261107-4ie
status: in_progress
objective: Parameterize durable funding-block D-04 qualification workload and run native candidate evidence.
---

# Quick Plan

## Scope

1. Parameterize the durable funding-block runtime workload through environment variables while preserving current defaults and fail-closed validation.
2. Add a repeatable D-04 scan entrypoint covering funding-block size, target seconds, renewal waterline, TCP and UDP durable authority, source conservation, settlement states, issuer occupancy, and idle-tail behavior.
3. Run the candidate matrix on the native Ubuntu host, preserving raw evidence and restoring temporary socket settings after every run.
4. Establish same-source CI regression evidence in the authenticated account’s existing VORTEX repository, using isolated qualification branches and a draft PR without modifying its main branch.
5. Update Phase 4 acceptance records only from observed evidence; keep D-04 unlocked when the matrix does not satisfy the durable authority bar.

## Acceptance

- Default runtime tests remain unchanged and pass.
- Candidate parameters are parsed with bounded, fail-closed validation.
- Scan output is reproducible, keeps failures, and identifies the exact commit and host.
- D-04 is locked only if durable TCP/UDP evidence demonstrates conservation, settlement, issuer occupancy, and idle-tail behavior across the required matrix; otherwise the remaining gap is documented truthfully.

## CI evidence repair within scope

Repair benchmark result parsing when Go benchmark names and metrics are separated by structured logs; preserve raw logs and numeric criteria. Pin the Phase 4 PR checkout to its actual head SHA so CI metadata and host evidence identify the tested source. The pre-existing VLink frozen-contract mismatch remains a separately recorded owner-review debt; do not alter approvals.

## Remaining native qualification sequence

After the live full-factorial job ends, run repeated dynamic-target 600ms renewal-response experiments for all three waterlines (256KiB block,30s horizon,8MiB floor), retaining errors/timeouts as failed availability observations. Do not increase the 200ms reservation wait. Use a separate clean 0c2ba71 worktree and preserve/restore socket settings. A provisional profile may receive formal-duration evidence before review, but remains unlocked until the full comparative matrix is audited. No native performance workloads run concurrently.

## Scope reconciliation

The later owner-approved `04-CLOSURE.md` governs Phase 4 acceptance. Durable
fixture evidence supplements safety review; a production durable issuer is not
a prerequisite for the Phase 4 parameter lock. Review must explicitly separate
idle demand shrink and block-tail settlement from issuer grant return. The
production Agent/Hub/PostgreSQL authority and its requalification remain Phase
5/6 handoffs. This supersedes the earlier production-durable-bar interpretation
in this quick plan without changing any measured result.

## Center-profile formal follow-up

The initial queued shell retained its launch-time 50% profile despite the later
on-disk edit. Its completed evidence remains 50%. Run the 30% center candidate
separately in a new directory on clean 0c2ba71, with the same 300 s warmup,
three 300 s trials and TLS comparison. Do not relabel, overwrite or combine
profiles. Runner PID 252907; log /tmp/vortex-phase4-formal-center.log.
