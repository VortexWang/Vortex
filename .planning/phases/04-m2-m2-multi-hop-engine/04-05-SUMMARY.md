---
phase: 04-m2-m2-multi-hop-engine
plan: 05
status: complete
---

# Plan 04-05 Summary

The Phase 4 benchmark entry now starts the real five-process runtime-backed
qualification before collecting library throughput data. Missing topology,
OPEN authorization logs or accounting JSONL causes a non-zero failure.

Verification on the current development host:

- `make phase4-runtime-mixed`: passed.
- `make phase4-m2-benchmark`: runtime qualification passed, benchmark smoke
  passed, UDP loopback measured 0% loss.
- Native Linux qualification remains required for final NFR claims; the current
  macOS benchmark numbers remain environment-limited.

The phase now has an executable runtime and qualification gate. Final NFR-003,
NFR-005 and D-04 status still requires the dedicated native three-relay mixed
load run described by the restart context.
