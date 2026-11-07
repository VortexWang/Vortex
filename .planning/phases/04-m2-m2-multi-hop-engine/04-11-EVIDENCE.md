---
phase: 04-m2-m2-multi-hop-engine
plan: 11
status: partial-slice-verified
source_sha: fc9013e
---

# Plan 04-11 Evidence — D-04 Scan and Formal Qualification Gate

## Current evidence reconciliation — 2026-11-07

The full native dynamic matrix completed 27 combinations x 3 repeats on clean
`2543fb745d942ddf710c02d502a3675b9b3c7fe9`. Independent raw workload,
flow/source accounting, consumption formula and 65-second idle audits pass for
all 81 trials. Summary SHA256:
`135a289cdf96d46c4b3668befa45e87c289403c669541fe7fb5b1932abc4566a`.

Clean `0c2ba71c0653ca1c806dc3df1db93d847544011b` completed the separate
256 KiB / 30 s / 50% formal candidate: 300 s warmup, three 300 s trials,
independent TLS, 16 streams and actual 100 Mbps UDP. The gate passes; SHA256:
`5be4202a989dcb005011abe0f03e0184610519f2cf2b35c861829f7c6204baef`.
The 30% center candidate is now receiving a separate formal run; it must not
inherit the 50% result. Nine 600 ms renewal pressure trials failed availability
and remain raw failures. The 200 ms reservation deadline is unchanged.

The current comparative review is [04-44-D04-PARAMETER-REVIEW.md](04-44-D04-PARAMETER-REVIEW.md).
`locked=false` and `gaps_found` remain. Formal performance acceptance, D-04
parameter lock and persistent authority are retained as user-required debts;
production Agent/Hub/PostgreSQL authority remains owned by Phase 5/6.
Historical results below retain their original source and measurement scope.

## Implemented

`tools/phase4/d04_scan.py` produces the controlled 3 x 3 x 3 matrix:
64/256/1024 KiB blocks, 10/30/60 second grant targets and 20/30/50 percent
waterlines, with an 8 MiB minimum budget and required raw observations. Every
case is explicitly `pending-evidence`; no candidate is locked by generation of
the matrix.

`tools/phase4/qualification_gate.py` is a fail-closed final gate. It requires
runtime-authority and independent TLS reports marked pass, host proof of
isolated resources and a dedicated link, at least 60 seconds of warmup, and at
least three trials. Missing or invalid reports are recorded and return nonzero.

## Verification

- `python3 tools/phase4/d04_scan.py --output /tmp/vortex-d04-matrix.json`
  generated 27 cases; matrix assertions confirmed all remain unlocked.
- The gate was run with missing reports and `0` trials/seconds; it returned
  exit 1 and wrote a `not-qualified` report listing every missing criterion.
- `DOCKER_CONTEXT=orbstack make phase4-m2-check` and the 04-10 race/Docker
  checks remain green on the preceding clean runtime SHA.

## Qualification Result

No eligible native host evidence, runtime-authority workload, independent TLS
pass report, 60-second warmup or three 300-second trials were available in this
workspace. D-04 candidates remain unlocked. NFR-003 and NFR-005 remain
`not-qualified`; Docker and hosted Actions remain regression evidence only.
