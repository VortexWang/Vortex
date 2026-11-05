# Phase 4 performance gate evidence

## Result

The dedicated-host performance gate passes with no reasons. This is the
NFR-003/005 performance qualification only; D-04 and the other Phase 4 closure
items are not approved by this result.

- Tested commit: `fe2a4ba7c4fca09362aacd7e8d20dbf7b74cb0f9`, clean worktree.
- Host: `43.254.164.7`, Ubuntu 24.04.5, native x86_64, 12 logical CPUs.
- Shared semantic manifest: `48572f6a76c2893d77714419d512982d39e0cef6a35937d575dcbae244791158`.
- Runtime authority enabled; source credit conserved and settled in all runs.
- Each run used 16 TCP streams and 300 seconds per phase. Warmup also used
  300 seconds so its semantic workload identity matches the three trials.

| Run | Mixed TCP Mbps | UDP offered Mbps | UDP loss |
| --- | ---: | ---: | ---: |
| Warmup | 1031.85 | 99.998400 | 0 |
| Trial 1 | 1034.71 | 99.998496 | 0 |
| Trial 2 | 1032.01 | 99.998528 | 0 |
| Trial 3 | 1030.56 | 99.998528 | 0 |
| Independent TLS mixed | 1402.42 | 99.997952 | 0 |

Trial 1 / independent TLS ratio is `0.7378017478679892`, above 0.70.
The 64 MiB binary-integrity/half-close probe, forged OPEN, direct bypass,
and middle-hop failure checks all report pass.

## Report correction provenance

The original comparison report was not-qualified despite meeting all metrics:
the evaluator looked for `tls_mixed.udp_loss` instead of
`tls_mixed.mixed.udp_loss`. Its default of 1 treated the missing top-level
field as total packet loss. The corrected evaluator was run on the existing
raw measurement files; the measurements and their tested commit were not
rewritten. The report includes `report_evaluator_sha256` to identify the
corrected evaluator separately from the tested binary commit.

The previous report and checksum list were preserved before regeneration.
Both dedicated-host and local executions of `qualification_gate.py` return:

```json
{"reasons": [], "schema": 1, "status": "pass", "trials": 3, "warmup_seconds": 300}
```

Remote evidence is under
`/root/VORTEX-phase4-runtime-authority/artifacts/phase4/formal/`.
The local review copy is under `artifacts/phase4/formal-fe2a4ba/`; its
`re-evaluation/` directory contains the original failing report, checksum list,
and corrected evaluator. Large raw accounting event logs remain on the host.

## Corrections to the debugging record

The stream write serialization change has a passing flow-control/FIN
regression test, 25 repeated targeted runs, VLink/relay race checks, and the
full Engine unit suite. The subsequent native 64 MiB probe passed. This does
not independently prove that the original remote reset had only that cause.

Earlier target-lifecycle diagnoses were incorrect: `topology-smoke.sh` already
runs the full mixed workload and captures its target snapshots before teardown.
The added post-teardown target restart and replacement accounting totals are
reverted in the final source, restoring independent target-snapshot accounting.
The measured commit still includes those now-reverted reporting changes; this
qualification must not be described as a new runtime execution of the later
reporting-only correction commit.

The host's temporary `net.core.rmem_max` and `net.core.wmem_max` values were
67108864 during measurement and are recorded in host/semantic evidence. Both
were verified restored to 212992 after completion.

## Verification of final reporting changes

Six Python tests pass, including nested zero loss acceptance, excess loss
rejection, missing loss rejection, diagnostic-mode rejection, and gate checks.
The shell entry point passes `sh -n`; the copied real evidence passes the local
gate independently.
