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
conditions are part of the measured workload identity. Runtime trial reports
also require source credit conservation and settled state at shutdown.

Focused gate tests, formal-config generation, Python and shell syntax checks,
and relevant Engine package tests passed. Dedicated-host formal execution must
still be performed and recorded separately after deployment.

The dedicated-host comparison stopped during its 64 MiB TCP binary-integrity
probe. Per-stream DATA and FIN were not serialized across the separate data and
control writer queues, allowing a concurrent CloseWrite to overtake DATA
waiting for flow-control credit. The MuxStream now serializes WriteData,
WriteDataFrames, and CloseWrite and rejects DATA after FIN. The regression test,
25 repeated targeted runs, VLink/relay race checks, and the complete Engine Go
test suite pass. Deploy the new clean SHA and rerun comparison and final gate
before claiming the performance gate is complete; restore temporary UDP socket
buffer sysctls afterward.
