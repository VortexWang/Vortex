---
phase: 03-m1-vlink-m1-vlink-engine
plan: 06
status: in-progress
requirements: [NFR-002, NFR-004, NFR-007]
commits: [56926ff]
---

# Plan 03-06 Summary: Reproducible Phase Verification

Added bounded `FuzzReadFrame` coverage, an integration test for unauthorized UDP OPEN/DGRAM behavior, and the shared `make phase3-vlink-check` entry point. The target runs fuzz smoke, the full race suite, a Linux Docker vlink test binary, and benchmark metadata capture. Docker uses a scratch image built for the detected native arm64/amd64 architecture and the same test selection as local development.

Added pinned GitHub Actions workflow `phase3.yml` on Ubuntu 24.04 with Go 1.27.3, the same Make target, host metadata, and uploaded Phase 3 evidence. The hosted workflow still needs to run on the evidence commit that records this native-host result.

The benchmark now records host metadata and raw results for direct TLS baseline, TCP tracer throughput, forward/reverse-established Mux throughput, and client-connect-to-first-byte mux/pool samples. TCP DATA frames are capped so the full VLink frame fits one TLS 1.3 record; tracer frame reads and writes reuse connection-local buffers. The Mux benchmark raises only its WINDOW limiter to keep the synthetic high-rate workload from measuring the abuse limiter instead of path direction.

A 3 x 10 sample run on the provided Ubuntu 24.04 KVM VM at implementation commit `0143cdd` measured 761.67 MB/s forward tracer throughput (70.01% of its TLS baseline), a 3.74% reverse-established Mux delta, and 198/195 us mux/pool first-byte overhead. Raw output and metadata are recorded under `artifacts/phase3/benchmark/20261103-0143cdd-ubuntu-kvm*`. These are numeric candidate passes only: the host is virtualized, the topology uses loopback, and the production accounting gate is disabled. The evidence commit and hosted Actions run remain pending; no formal full-system qualification is claimed.

## Verification

- `make phase3-vlink-check` passed locally: two-second fuzz smoke, `go test -race ./...`, Ubuntu Linux Docker integration tests, and benchmark smoke.
- Fuzz smoke executed 254,180 cases in two seconds on the latest recorded local check.
- `cd engine && go vet ./...` passed.
- `git diff --check` passed.
- `0143cdd` passed `go test -race ./...` and `go vet ./...` on the Ubuntu KVM host.
- The 3 x 10 Ubuntu candidate benchmark passed numeric thresholds for NFR-002, NFR-004, and NFR-007; see the evidence manifest and raw output.

## Scope Notes

- This phase has a reproducible local/Actions command, but GitHub Actions has not yet run on the evidence commit.
- Docker smoke remains functional evidence only; final performance evidence must preserve the KVM, loopback, and disabled-accounting limitations.

## Self-Check

**Self-Check: PASSED**

- [x] Summary reflects the implemented files and verification commands.
- [x] Commit hash is recorded in frontmatter.
- [x] Residual native-host performance qualification is stated explicitly.
