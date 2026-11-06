---
quick_id: 20261106-phase4-d04-harness-cleanup
status: in-progress
---

# Phase 4 D-04 Harness Cleanup

Make the Phase 4 workload driver's TCP selector cleanup idempotent so a real
D-04 one-factor scan cannot fail after a successful workload because a socket
was already closed or unregistered. Preserve the fail-closed accounting and
qualification semantics, then rerun the scan entrypoint and record whether the
current host can produce complete candidate evidence.

Verification:

- `python3 -m py_compile tools/phase4/mixed_workload.py`
- `python3 -m unittest discover -s tools/phase4 -p 'test_*.py'`
- one-factor D-04 scan output has complete case results or a concrete scoped
  environmental failure.
