---
phase: 03-m1-vlink-m1-vlink-engine
plan: 05
status: complete
requirements: []
commits: [c10a890]
---

# Plan 03-05 Summary: Abuse Limits and Observability

Added per-link, per-control-frame token buckets for OPEN, OPEN responses, FIN/RST, PING/PONG, WINDOW and GOAWAY. Defaults are finite and configurable; invalid bounds are rejected. Excess control frames are dropped, OPEN excess resets only its stream, and the mux closes after a cumulative violation threshold. Structural parser and identity failures continue to close the link immediately. The mux now records protocol, rate and stream-violation classes and emits link-close events through the injected observer.

Added a bounded in-memory observer with counters for frame/byte direction, close and rejection classes, active streams, pool state and accumulated window wait. Structured events allow only safe event kinds and low-cardinality peer role/frame/error fields plus opaque trace/link/correlation IDs. Retention is bounded, shutdown rejects further writes, and no normal DATA/DGRAM packet event is emitted.

## Verification

- `cd engine && go test -race ./vlink -run 'Test(Limit|Observability)'` passed.
- `cd engine && go test -race ./...` passed on local macOS arm64.
- `cd engine && go vet ./...` passed.
- `git diff --check` passed.

## Scope Notes

- The observer is an Engine boundary; exporting counters to Prometheus or a structured log backend remains a runtime integration concern.
- Rate values and event retention are explicit configuration/defaults and should be reviewed with deployment capacity before production rollout.
- Docker, benchmark evidence, fuzz smoke and GitHub Actions parity remain in plan 03-06.
