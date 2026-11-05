---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: 01
status: complete
completed: 2026-11-05
commit: 9e67f96
---

# Wave 1 Summary

Implemented the first startable Agent/Hub tracer boundary. `vortex-node` creates a fresh boot_id, records durable boot/heartbeat events and connects to a clearly test-only Hub protocol. Agent and Hub use independent `modernc.org/sqlite` WAL databases with `synchronous=FULL`, bounded connections, busy timeout, schema versioning and runtime SQLite evidence. Engine-facing code has no SQLite dependency.

The control contract is now draft 1.1.0 with registration and control error fields; it remains draft because canonical payload, signing, expiry and full lifecycle semantics require the next control-contract review.

## Verification

- `make phase5-wave1-check` passed locally on Go 1.27.3 arm64.
- `go test ./...` and `go vet ./...` passed, including `test/phase5` start/stop tracer integration.
- `protoc` generated the control descriptor successfully.
- Evidence: `artifacts/phase5/wave1/report.json`, `host.txt`, `go-test.log`, `go-vet.log`.
- GitHub Actions workflow `.github/workflows/phase5.yml` runs the same Make target on the same commit after push.

## Scope boundary

This is not production mTLS enrollment, a PostgreSQL backend, durable Hub authority or full accounting handoff. Those remain in Plans 05-02 through 05-04 and Phase 6 as defined by the Phase 5 context.
