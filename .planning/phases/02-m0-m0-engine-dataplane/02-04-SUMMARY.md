---
phase: 02-m0-m0-engine-dataplane
plan: 04
status: complete
completed: 2026-11-03
requirements: [ENG-006, ENG-007, ENG-008, ENG-009]
---

# Plan 02-04 Summary: Bounded UDP Admission

## Delivered

- Added finite defaults of 4,096 concurrent UDP sessions globally and 64 per source IP; callers can configure limits.
- Session admission is checked under the session lock before dialing the target or creating a map entry and response goroutine.
- A new client datagram is dropped when a limit is reached. `OnSessionLimit` provides an optional notification callback, invoked outside the session lock.
- Session counts are released on idle expiry, read/write failure, and cancellation cleanup.
- Added race-enabled coverage for per-client and global saturation, limit reporting, negative configuration, and capacity reuse after expiry.

## Verification

- `cd engine && go test -race ./relay` — passed.
- `make phase2-engine-check` — passed locally, including OrbStack Linux container execution.
- GitHub Actions for the implementation commit is pending and must be recorded after push.

## Notes

The limits are safety defaults, not product quota policy. Gate and reservation accounting remain Plan 02-05 work. The benchmark remains Plan 02-06 work.
