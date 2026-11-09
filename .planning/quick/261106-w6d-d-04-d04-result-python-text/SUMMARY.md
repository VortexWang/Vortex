---
quick_id: 261106-w6d
status: complete
completed: 2026-11-06
commit: b96b1fe
---

# Quick Summary

The D-04 runner now validates and emits renewal attempt/merge counts and
merge rate, idle-tail reclaim totals, and peak active/held/pending/unknown
occupancy in each `d04-result.json`. Two Python tests cover valid summaries and
malformed counts.

Validation passed: `python3 -m unittest discover -s tools/phase4 -p
'test_*.py'` (23 tests) and Python bytecode compilation. This is diagnostic
evidence for the process-local issuer only; it does not lock D-04 parameters
or close native performance and durable-authority debts.
