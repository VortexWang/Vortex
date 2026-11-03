# Phase 3 Evidence

`make phase3-vlink-check` is the shared local/Actions entry point. It runs the Go unit and race suites, a bounded fuzz smoke, a Linux Docker integration smoke, and a benchmark metadata capture.

Docker and hosted Actions runs are functional evidence only. NFR-002 throughput, NFR-004 reverse/forward delta, and NFR-007 first-byte latency remain unqualified until an approved native Ubuntu 24.04 host records the required topology, CPU, memory, tool versions, raw results, and commit SHA.
