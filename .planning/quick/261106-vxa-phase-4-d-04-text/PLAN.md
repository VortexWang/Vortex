---
quick_id: 261106-vxa
objective: 补充 Phase 4 D-04 进程内观测：续申合并、闲置尾部回收与占用峰值，并加入回归测试；保持参数未锁定、生产持久化权威为后续债务
status: in_progress
---

# Quick Plan

1. Extend the opt-in `CreditObservations` snapshot with bounded counters for
   renewal attempts/merges, idle-tail reclaim, and peak pending/unknown
   occupancy. Keep the metrics explicitly process-local and diagnostic.
2. Update the injected renewal and credit-session paths to maintain those
   counters without changing credit state transitions or candidate defaults.
3. Add focused tests for concurrent renewal coalescing and idle-tail reclaim,
   then run the accounting race suite and repository validation.
4. Record the evidence boundary: observations improve D-04 review only;
   remote merge, durable funding occupancy, idle return proof, formal
   qualification, and parameter locking remain open.

## Verification

- `go test -race ./runtime/accounting ./runtime/relay ./cmd/vortex-relay`
- `go vet ./...`
- `python3 -m unittest discover -s tools/phase4 -p 'test_*.py'`
- `DOCKER_CONTEXT=orbstack make phase4-m2-check`
