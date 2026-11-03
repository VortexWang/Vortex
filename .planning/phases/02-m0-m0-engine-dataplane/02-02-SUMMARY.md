---
phase: 02-m0-m0-engine-dataplane
plan: 02
status: complete
completed: 2026-11-03
requirements:
  - ENG-004
  - ENG-005
---

# Plan 02-02 Summary: Linux TCP Splice

Added a Linux amd64/arm64 platform copy path for bare `*net.TCPConn` pairs. It stages source data through a nonblocking kernel pipe, drains partial splice writes before reading more, counts bytes only after the destination socket accepts them, and propagates EOF with `CloseWrite` after the pipe is empty. Other platforms and non-TCP connections keep the portable relay path.

The Linux-specific loopback test sends data through `copyTCPPlatform`; it passes only when the Linux splice path successfully completes the transfer. Existing half-close, cancellation, and metering-failure tests also execute in the Linux scratch test container.

## Verification

- `make phase2-engine-check` — passed native race tests and Linux arm64 scratch-container tests.
- Linux scratch-container tests — 4 passed, including `TestTCPPlatformSelectsSpliceForBareTCP`.
- Linux amd64 and arm64 test binaries cross-compiled successfully.
- Throughput benchmark not run; this plan establishes correctness of the syscall path, not the 700 Mbps target.

## Limitations

Linux syscall failures are returned and close both relay endpoints. No buffered fallback is attempted after a source read may have staged bytes in the pipe; this avoids loss or duplicate forwarding. UDP, gates/rate limits, and usage reservation semantics remain future M0 work.
