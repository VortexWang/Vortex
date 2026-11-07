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
The 30% center candidate completed a separate formal run (see follow-up below); it does not inherit the 50% result. Nine 600 ms renewal pressure trials failed availability
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

## Center candidate formal follow-up — 2026-11-07

Clean `0c2ba71c0653ca1c806dc3df1db93d847544011b` also completed a separate
256 KiB / 30 s / 30% candidate run in
`/root/Vortex-phase4-final-0c2ba71/artifacts/phase4/formal-provisional-256k-30s-30pct`.
The 300-second warmup and three 300-second trials use semantic digest
`84a3e5c9e6efe6b8496ba6cd1a33fe92ecf9d5cad3c4e0182a14e6a03fec2de6`.
Trial TCP throughput was 628.675400 / 631.912733 / 636.537656 Mbps; actual
UDP rates were 99.998368 / 99.998112 / 99.997408 Mbps with zero loss. Each
trial conserved and settled source credit. Independent TLS mixed TCP measured
568.820579 Mbps; the runtime/TLS ratio was 1.119048221. The machine gate
returned `pass`, runner exit was 0, and socket maxima restored to 212992 each.

The gate SHA256 is
`5be4202a989dcb005011abe0f03e0184610519f2cf2b35c861829f7c6204baef`.
(The 30% and 50% gate files contain the same verdict fields and therefore have
the same hash; they do not identify a workload.) The 30% comparison SHA256 is
`9a427d3e8fc5e1ada784c82218ceafbd550f563570e5ef9f4e0f3a6e2557d81f`;
trial-1 host SHA256 is
`dffe1deb3456fb01f5d665163b2d24bf3ee2f2b3147efd794a964a3a25bfb206`.

The center candidate now has its own formal-duration evidence. The comparison
supports recommending 256 KiB initial / 4 MiB maximum block, 8 MiB demand
floor, 30 s horizon with explicit 375000000-byte cap, 30% renewal waterline,
and unchanged 200 ms reservation wait for this Engine/in-process adapter.
This is a measured recommendation, not a production parameter approval.
The user-required formal qualification and D-04 lock debts remain retained;
`locked=false` / `gaps_found` are unchanged. Production persistence authority
and requalification remain Phase 5/6 handoffs.
