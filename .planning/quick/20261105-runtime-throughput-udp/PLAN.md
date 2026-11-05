---
quick_id: 261105-rudp
status: complete
---

# Runtime Throughput and UDP Loss

Reproduce the native high-load failure, then reduce avoidable data-plane
overhead while preserving byte accounting and bounded UDP behavior.

- Increase gated TCP copy chunks and VLink windows/queues without changing
  protocol limits.
- Raise UDP socket buffer requests for source and target sockets.
- Run race/unit checks and native 4-flow, 16-flow, and mixed-load diagnostics.
- Keep qualification status honest until the formal workload passes.

Implementation completed in the current working tree. The native host retest
is the acceptance step; no performance requirement is marked complete from
unit tests alone.
