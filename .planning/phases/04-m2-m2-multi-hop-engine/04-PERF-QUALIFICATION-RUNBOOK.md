# Phase 4 Performance Qualification Runbook

This runbook is the remaining gate for Phase 4. The current Engine and VLink
tests prove boundary correctness, but the repository does not contain a
runtime three-relay deployment or a production accounting process that can
produce the required qualification evidence.

## Required host

- Ubuntu 24.04 LTS, native amd64 or arm64, dedicated 4 vCPU / 8 GiB or better.
- Fixed commit under test, clean worktree, Go 1.27.3 and Docker versions recorded.
- No macOS virtualization or QEMU results may be used for the performance claim.

## Required workload

1. Run source, relay 1, relay 2, relay 3 and destination as separate native
   processes or containers on the authorized path. The client must not have a
   route that bypasses a relay.
2. Run concurrent long-lived TCP traffic and 1,200-byte UDP traffic at 100 Mbps.
3. Use the production accounting gate and immutable billing snapshot path;
   record successful-write bytes, reservations, commits, unknown outcomes and
   queue/window/downstream drop counters.
4. Capture same-path TLS baseline and three-relay throughput on the same host,
   then calculate `multi_hop / same_path`. NFR-003 requires at least 400 Mbps
   and at least 70% of the same-path baseline.
5. Sustain the workload long enough to expose queue growth and authorization
   churn. Preserve raw logs, environment metadata and checksums.

## Acceptance outputs

- TCP ordering/checksum and no-bypass evidence.
- UDP received/lost counts with loss percentage at or below 0.1%.
- Throughput, baseline ratio, first-byte latency and queue occupancy.
- Accounting reconciliation showing no duplicate commit and no release of an
  unknown reservation upper bound.
- D-04 proposed pre-debit block, watermark and renewal values with reviewer
  decision. Until this review, D-04 remains a candidate rather than a default.

The existing loopback UDP result (`0.0000%` loss) and first-byte result are
boundary evidence only. They must be retained alongside, not substituted for,
this run.
