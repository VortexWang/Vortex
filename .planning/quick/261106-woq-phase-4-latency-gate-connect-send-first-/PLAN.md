---
quick_id: 261106-woq
objective: 增强 Phase 4 latency 采集与 gate：记录并校验 connect/send/first-byte/complete/close 事件时间线，缺失时 fail-closed
status: in_progress
---

# Quick Plan

1. Extend the latency probe with per-sample monotonic event timestamps and
   derived intervals for connect, send, first byte, full echo, and close.
2. Make the pool/mux latency gate validate timeline shape, ordering, finite
   non-negative intervals, and sample-count alignment before accepting a mode.
3. Update gate tests and documentation; run the full Phase 4 Python suite and
   relevant Go/Docker regressions without changing performance thresholds.
