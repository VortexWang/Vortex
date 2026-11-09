---
quick_id: 261106-w6d
objective: 把新增 D-04 续申合并与占用观测纳入 d04-result 汇总并增加 Python 校验；不改变参数锁定状态
status: complete
---

# Quick Plan

1. Validate the new observation counters in `tools/phase4/d04_run.py` and
   expose merge rate, peak pending/unknown grants, and idle reclaim totals in
   each trial result.
2. Add focused Python tests for valid and malformed observation summaries.
3. Run the Python gate and the affected Go/Python regression checks; record
   the result without promoting any D-04 candidate.
