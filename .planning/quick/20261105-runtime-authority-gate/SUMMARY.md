# Runtime authority qualification gate

The formal Phase 4 fixture now runs with pinned runtime authorization on every
route node and an explicit finite source-credit Gate on source. The default
diagnostic fixture remains qualification-only. Reports mark NFR
performance as passed only when formal mode, runtime authority, duration, flow
count, throughput, actual UDP offered load/loss, and runtime/TLS ratio satisfy
the configured thresholds.

The gate binds runtime, TLS, warmup, and three unique trial reports to the same
clean commit and semantic workload, verifies Ubuntu 24.04 native host facts,
and fails closed on missing or mismatched evidence. D-04 and the other remaining
Phase 4 requirements remain separate.

UDP socket sysctl limits are included in host and semantic evidence so buffer
conditions are part of the measured workload identity.

Focused gate tests, formal-config generation, Python and shell syntax checks,
and relevant Engine package tests passed. Dedicated-host formal execution must
still be performed and recorded separately after deployment.
