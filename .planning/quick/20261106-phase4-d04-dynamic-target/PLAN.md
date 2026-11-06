---
quick_id: 20261106-phase4-d04-dynamic-target
status: complete
---

# Phase 4 Dynamic D-04 Target Scan

Pass the target-seconds factor into the runtime `CreditConfig.TargetSeconds`
field instead of measuring only a fixed byte ceiling. Record the bounded credit
timeline's dynamic-target range in every trial result, add evaluator coverage,
and rerun the full one-factor scan.

Verification:

- `python3 -m unittest discover -s tools/phase4 -p 'test_*.py'`
- every D-04 trial records a nonzero dynamic-target timeline and exact source
  conservation;
- the candidate summary remains `locked=false` until review.
