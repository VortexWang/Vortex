---
quick_id: 261106-wd1
status: complete
completed: 2026-11-06
commit: b47c282
---

# Quick Summary

Added an authenticated runtime bridge regression for bounded early TCP DATA.
The test sends a prefix while OPEN is pending, confirms the remote stream does
not expose it before authorization, then authorizes the stream and verifies
exact target delivery with settled source and destination accounting. Existing
VLink tests retain strict UDP OPEN/OPEN_OK gating.

Validation passed:

- targeted `go test -race ./runtime/relay -run TestBridgeOpeningConnBuffersTCPUntilRemoteAuthorization -count=1 -v`
- `go test -race ./runtime/relay ./runtime/accounting ./vlink ./cmd/vortex-relay`
- `go vet ./...`
- 23 Phase 4 Python tests

This closes only the finite early-data implementation slice. Controlled RTT,
native resource qualification, formal NFR-007 acceptance, D-04 locking, and
persistent authority remain open.
