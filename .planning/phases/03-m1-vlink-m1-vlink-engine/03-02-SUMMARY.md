---
phase: 03-m1-vlink-m1-vlink-engine
plan: 02
status: complete
requirements: [ENG-010, ENG-016, ENG-019, ENG-025]
commits: [e8c8d44, 6729f52, a74c5bc]
---

# Plan 03-02 Summary: Authenticated Multiplexing and Flow Control

Added TLS 1.3 configuration and verified-chain/SAN policy hooks, bounded peer boot/generation and HELLO replay state, and conservative lease intersection using `q + ttl - max(0.1*ttl, 10s)`. The lease tracker rejects RTT above 10 seconds, stale/duplicate revisions, duplicate request IDs/nonces, elapsed intersections, and request-time rollback.

Added the stream lifecycle table and an authenticated mux constructor. A VLink link can carry independent streams over one connection; OPEN IDs follow initiator parity and are never reused, TCP DATA sent while OPEN is pending is buffered only at the receiver up to a configured bound, UDP DGRAM is admitted only after OPEN_OK, and stream-local violations reset only that stream. Per-stream and aggregate byte windows support cancellable TCP waits, non-blocking UDP drops, WINDOW updates, overflow checks, and close wakeups. Liveness sends nonce-bound PINGs after 3 seconds idle, reports a 3-second timeout, and closes at 12 seconds without valid traffic.

## Verification

- `cd engine && go test -race ./vlink -run 'TestIdentity|TestFreshness'` passed.
- `cd engine && go test -race ./vlink -run 'Test(Stream|Window)'` passed.
- `cd engine && go test -race ./vlink -run 'Test(Window|Liveness)'` passed.
- `cd engine && go test -race ./...` and `cd engine && go vet ./...` passed on local macOS arm64.
- Mux integration passed with two independent streams, authorization-gated OPEN_OK, per-stream/aggregate credit exhaustion and WINDOW wakeup.
- `BenchmarkStreamOpenToFirstData` smoke: 176625 ns/op for one local macOS arm64 iteration. This is a harness smoke value, not an NFR result.

## Scope Notes

- Revocation, current path membership, durable freshness lower bounds, and the monotonic lease request start are supplied by runtime callbacks/state; this package does not claim a durable production authority.
- `NewAuthenticatedMuxLink` is the public constructor and requires TLS 1.3, a verified peer chain, HELLO exporter binding, and injected peer checks before mux admission. Raw-connection construction is package-private for tests.
- Window updates are explicit through `MuxStream.Consume`; callers must return credit after consuming payloads. Phase 3 later plans add OPEN policy/UDP handling, pool/reverse links, abuse limits, metrics, and cross-host CI evidence.
