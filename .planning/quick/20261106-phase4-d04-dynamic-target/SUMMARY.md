---
quick_id: 20261106-phase4-d04-dynamic-target
status: complete
---

# Phase 4 Dynamic D-04 Target Scan

The D-04 runner now passes `PHASE4_CREDIT_TARGET_SECONDS` and records dynamic
target timeline ranges. Clean `e862ffe` completed all 21 short candidate
trials with exact source conservation, actual roughly 100 Mbps UDP and zero
loss. Each target factor reached the configured ceiling from the 8 MiB floor.

The scan remains process-local candidate evidence. D-04 is still unlocked;
remote merge, durable funding/idle-return proof and formal native-host
qualification remain outside this adapter.

Validation:

- `python3 -m unittest discover -s tools/phase4 -p 'test_*.py'` (21 passed)
- `python3 tools/phase4/d04_run.py --duration 5 --repeats 3 --rate-mbps 100` (21 trials)
