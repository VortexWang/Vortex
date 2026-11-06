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
4. Update Phase 4 acceptance records only from observed evidence; keep D-04 unlocked when the matrix does not satisfy the durable authority bar.

## Acceptance

- Default runtime tests remain unchanged and pass.
- Candidate parameters are parsed with bounded, fail-closed validation.
- Scan output is reproducible, keeps failures, and identifies the exact commit and host.
- D-04 is locked only if durable TCP/UDP evidence demonstrates conservation, settlement, issuer occupancy, and idle-tail behavior across the required matrix; otherwise the remaining gap is documented truthfully.
