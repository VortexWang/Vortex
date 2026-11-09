---
quick_id: 20261106-phase4-d04-harness-cleanup
status: complete
---

# Phase 4 D-04 Harness Cleanup

Made TCP selector cleanup idempotent in `tools/phase4/mixed_workload.py` after
the first scan exposed a real repeated-unregister failure. The clean SHA
`ae858f5` then completed all 21 short D-04 candidate trials with exact source
conservation and zero UDP loss. A 15-second run failed closed under the current
Darwin host's sustained mixed load; both the successful candidate scan and the
failure boundary are recorded in `04-38-D04-CANDIDATE-SCAN-EVIDENCE.md`.

D-04 remains unlocked. The process-local scan does not prove remote issuer
merge, durable funding, or formal performance qualification.

Validation:

- `python3 -m py_compile tools/phase4/mixed_workload.py`
- `python3 -m unittest discover -s tools/phase4 -p 'test_*.py'` (20 passed)
- `python3 tools/phase4/d04_run.py --duration 5 --repeats 3 --rate-mbps 100` (21 trials)
