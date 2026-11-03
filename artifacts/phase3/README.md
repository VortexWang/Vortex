# Phase 3 Evidence

`make phase3-vlink-check` is the shared local/Actions entry point. It runs the Go unit and race suites, a bounded fuzz smoke, a Linux Docker integration smoke, and a benchmark metadata capture.

The VLink tracer throughput and direct TLS baseline are measured by `BenchmarkVLinkTCPThroughput`. Its Mux forward/reverse comparison uses separate established TLS links: forward has ingress dial the node, while reverse has the node dial ingress and ingress open the stream. `BenchmarkVLinkFirstByte` measures mux OPEN-to-data and includes `LinkPool.Acquire` for the prewarmed-pool sample, then subtracts the median of nine loopback RTT samples for both link and target legs.

The reversed payload direction on the tracer is diagnostic only; NFR-004 uses the separate reverse-established Mux measurement. These loopback results are candidate measurements, not a substitute for the approved deployment path topology. NFR-007 includes a local client TCP connect but uses loopback link/target RTT samples rather than a real ingress, path, and egress-to-target network; those path legs still need qualification. Docker and hosted Actions runs remain functional evidence only.
