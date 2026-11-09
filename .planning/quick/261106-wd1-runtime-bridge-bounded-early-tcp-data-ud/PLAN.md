---
quick_id: 261106-wd1
objective: 补充 runtime bridge 延迟授权下的 bounded early TCP DATA 回归，证明授权前不写目标、授权后完整交付；保持 UDP OPEN_OK 门禁
status: complete
---

# Quick Plan

1. Add an authenticated runtime bridge test that sends a bounded TCP prefix
   while the remote OPEN is still pending and proves the target receives no
   bytes before authorization.
2. Authorize the remote stream, drain the prefix into a target connection,
   and assert exact payload/order and settled source accounting.
3. Run relay/accounting race tests, the full Go race/vet suite, Python gates,
   and the Docker Phase 4 regression; record the evidence boundary separately
   from formal RTT/performance qualification.
