---
phase: 04-m2-m2-multi-hop-engine
plan: 01
status: complete
---

# Plan 04-01 Summary

Implemented the first runtime boundary for the restarted Phase 4:

- `engine/runtime/config` strictly decodes JSON, validates role/node/listener/
  path fields and loads TLS 1.3 mTLS material.
- `engine/runtime/accounting` provides a bounded in-process Gate with stable
  batch IDs, JSONL events, exactly-once resolution and unknown upper-bound
  retention.
- `engine/cmd/vortex-relay` starts from validated config and handles signal
  shutdown as the common process entry point for source/relay/destination roles.

Verification:

```text
cd engine && go test -race ./runtime/... ./cmd/vortex-relay
cd engine && go build ./cmd/vortex-relay
```

Both passed. Network forwarding is intentionally deferred to Plan 04-02.
