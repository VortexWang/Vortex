# Runtime authority qualification gate

## Objective

Connect the Phase 4 formal workload to a funded runtime authority and make the
runtime and independent TLS reports satisfy the existing fail-closed gate only
when the formal workload and measured thresholds pass.

## Scope

- Generate formal source configs with bounded `source_credit` and explicit
  runtime authorization; preserve diagnostic defaults.
- Derive report qualification fields from formal workload evidence and host
  metadata instead of hard-coded diagnostic values.
- Document formal invocation and the retained fail-closed limitations.
- Run focused tests, then repeat the native-host formal qualification.

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
- Dedicated-host execution is pending after this commit is deployed.
