# Runtime authority qualification gate

## Objective

Connect the Phase 4 formal workload to a funded runtime authority and make the
runtime and independent TLS reports satisfy the existing fail-closed gate only
when the formal workload and measured thresholds pass.

## Scope

- Generate formal configs with bounded source `source_credit` and explicit
  pinned runtime authorization on every route node; preserve diagnostic defaults.
- Derive report qualification fields from formal workload evidence and host
  metadata instead of hard-coded diagnostic values.
- Document formal invocation and the retained fail-closed limitations.
- Run focused tests, then repeat the native-host formal qualification.

## Follow-up: TLS Comparison Half-Close

- Reproduce the 64 MiB binary-integrity failure in the independent runtime
  comparison and identify why the route resets the stream before echo completes.
- Serialize per-stream DATA writes and FIN, then add a regression test where a
  DATA write is blocked on flow-control credit while CloseWrite is requested.
- Re-run VLink/relay race checks and the Engine test suite before deploying the
  new clean SHA to the dedicated Ubuntu host.
- Re-run independent TLS comparison and final qualification gate; restore the
  temporary host UDP socket buffer limits afterward.

## Verification

- Diagnostic generation remains `qualification-only` and not qualified.
- Formal configs validate with a funded source authority.
- Gate passes only with formal runtime/TLS reports, three trials, and 60-second
  warmup evidence.

## Completion

- Added an explicit formal fixture mode with bounded source credit and retained
  qualification-only diagnostic defaults.
- Runtime and TLS reports now include authorization, commit, workload identity,
  trial identity, and measured acceptance metrics.
- The fail-closed gate checks host eligibility and actual warmup plus three
  unique, passing trial reports.
- Focused unit, config generation, syntax, and Engine package checks passed.
- Dedicated-host formal execution passed warmup and three runtime trials on
  commit `6bd774f`; independent TLS comparison exposed a TCP stream ordering
  failure during the 64 MiB half-close probe.
- A first warmup attempt exposed relay OPEN rejection under the default
  fail-closed static policy. Added pinned `runtime_authority` config for each
  route node; source traffic remains gated by finite source credit.
- Host and semantic evidence records UDP socket sysctl limits because the first
  warmup showed kernel receive-buffer caps were driving observed packet loss.
- Runtime trial qualification requires a conserved and fully settled source
  credit state with zero live sessions, pending/unknown amounts, or denials.
- Root cause: DATA and FIN use separate writer queues, while one MuxStream had
  no write-side serialization. Concurrent CloseWrite could put FIN on the
  control queue while DATA was waiting for stream credit.
- Fix: serialize WriteData, WriteDataFrames, and CloseWrite with a per-stream
  mutex; reject local DATA after FIN. A focused regression and 25 repeated runs,
  VLink/relay race checks, and all Engine Go tests passed locally.
- Dedicated-host deployment and independent TLS comparison remain pending for
  the new commit; no Phase 4 overall completion is claimed.
