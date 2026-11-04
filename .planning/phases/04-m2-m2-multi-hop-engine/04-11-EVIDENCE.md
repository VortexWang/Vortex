---
phase: 04-m2-m2-multi-hop-engine
plan: 11
status: partial-slice-verified
source_sha: fc9013e
---

# Plan 04-11 Evidence — D-04 Scan and Formal Qualification Gate

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
