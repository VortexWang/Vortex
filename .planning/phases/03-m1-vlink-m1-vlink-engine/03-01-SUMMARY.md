---
phase: 03-m1-vlink-m1-vlink-engine
plan: 01
status: complete
requirements: [ENG-011, ENG-012, ENG-013, ENG-014, ENG-015]
commits: [7d97b4a, 6310aef, 477e749, d413951]
---

# Plan 03-01 Summary: VLink Contract and Authenticated TCP Tracer

Frozen the VLink 1.0.0 frame contract and SHA-256, added bounded frame and OPEN codecs, TLS 1.3 exporter-bound three-message HELLO with certificate node identity and injected peer freshness/policy verification, and implemented a single-stream TCP tracer. The tracer selects targets only through a local authorization callback, carries DATA and directional FIN, returns stable OPEN rejection classes, and offers an accounting-gated server path that reserves before writes and commits accepted bytes.

## Verification

- `cd engine && go test ./vlink -run 'Test(Frame|Codec)'` passed.
- `cd engine && go test ./vlink -run 'TestHello'` passed, including exporter mismatch, role binding, unsupported version, missing certificate SAN, stale generation and replay callback rejection.
- `cd engine && go test -race ./vlink -run 'TestTracer'` passed; local TCP endpoints completed request/half-close/response and the server exercised accounting reservations and metering.
- `cd engine && go test -race ./...` and `cd engine && go vet ./...` passed on local macOS arm64.
- `shasum -a 256 -c contracts/vlink/v1/encoding.sha256` and `git diff --check` passed.

## Scope Notes

- The tracer is deliberately single-stream; multiplexing, aggregate/stream windows, link pools, reverse establishment, UDP, abuse controls, and cross-host Docker/Actions evidence remain in later Phase 3 plans.
- HELLO certificate chain, revocation, path membership, conservative clock and durable freshness checks are injected runtime policies. This plan does not claim those production authorities already exist.
- `ServeTCP` remains usable without accounting for isolated protocol tests; production callers that require quota enforcement use `ServeTCPWithGate`.
- The OPEN payload ceiling is 310 bytes so the contract admits the stated maximum of 16 visited node IDs plus an IPv6 client address.
- `d413951` removes the pre-existing local `.planning/state.json` snapshot from the committed tree while preserving it in the workspace and local Git exclude.
