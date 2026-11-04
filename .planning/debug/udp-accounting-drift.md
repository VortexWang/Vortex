---
status: verifying
trigger: "16-stream 60-second mixed TCP/UDP qualification has high UDP loss and target/accounting byte mismatch"
created: 2026-11-04
updated: 2026-11-04
---

# UDP and Accounting Drift

## Symptoms

- Expected: 16 TCP streams plus 100 Mbps UDP run for 60 seconds; target byte counters reconcile with destination accounting and UDP loss stays within the configured threshold.
- Actual: On clean `41278c4936605a8e901350ebea4ade8f15f58964`, `GOMAXPROCS=1`, event cap 10,000,000, the 60-second run completed its TCP client workload but qualification failed. Target counters recorded 49,888,428,056 TCP bytes and 737,938,828 UDP bytes. Destination accounting recorded 30,886,362,020 left-to-right and 30,886,026,744 right-to-left committed bytes.
- UDP loss: 10,616 of 624,907 datagrams (1.699%). Destination link summary attributed 656 drops to aggregate credit. Kernel receive-buffer errors were zero.
- Errors: destination logged one UDP target read timeout and one stream reset. The default one-million accounting-event cap exhausted on an earlier attempt; the 10-million attempt did not log event-cap exhaustion.
- Reproduction: `DOCKER_CONTEXT=orbstack BACKEND=docker GOMAXPROCS=1 VORTEX_QUALIFICATION_EVENT_LIMIT=10000000 MIXED_DURATION_SECONDS=60 MIXED_TCP_STREAMS=16 make phase4-runtime-mixed`.

## Current Focus

- hypothesis: contiguous accounting event slice growth causes long-run scheduling stalls, while the default Docker stop grace truncates evidence on shutdown.
- test: repeat the passing 16-stream/60-second run on a clean SHA and run GitHub Actions with the same code.
- expecting: accounting reconciles, UDP loss is below 0.1%, and all relay exits are clean.
- next_action: commit repairs, push, and collect same-SHA local Docker and Actions evidence.

## Evidence

- timestamp: 2026-11-04 — Full original 16-stream, 60-second output preserved in `artifacts/phase4/qualification/docker-41278c4-16x60-failed`; 2,943,943 committed event rows, 49.89 GB target TCP bytes, 30.89 GB destination accounting, 1.699% UDP loss. Destination alone recorded 656 stream-credit drops; the original diagnostic incorrectly called these aggregate-credit drops.
- timestamp: 2026-11-04 — 5-second 16-stream control passed with exact accounting reconciliation and 0% UDP loss. 20-second 16-stream control also reconciled accounting exactly; UDP loss was 0.837%, fully classified as 1,743 aggregate-window drops.
- timestamp: 2026-11-04 — Failure event IDs are contiguous through 2,943,943 with no accounting errors in relay logs. `docker compose stop` had no configured grace period; probable short JSONL flush at shutdown. Added a two-minute relay stop grace for validation.
- timestamp: 2026-11-04 — With 2-minute relay stop grace, the 60-second-per-phase run wrote 6,015,013 rows and reconciled exactly: accounting 53,980,247,076 / 53,980,015,476 bytes; target-derived expectation matched. UDP loss remained 8,822/624,944 (1.412%), classified as stream and aggregate window drops.
- timestamp: 2026-11-04 — Bounded credit/queue changes alone retained 5,676 stream-credit drops (0.908%) at 60 seconds. Replacing contiguous event storage with 4,096-entry chunks on the same candidate configuration produced 0/624,959 UDP loss, zero classified drops, TCP 3,159.85 Mbps, exact accounting at 52,526,929,796 bytes each direction, and 5,879,776 events. Full Engine race suite passed. Clean-SHA replay remains pending.

## Resolution

- Evidence flush: two-minute relay stop grace, nonzero-exit rejection, and fatal evidence I/O errors.
- Runtime scheduling: bounded event chunks avoid full-history append copies; JSONL captures chunk headers instead of copying all events. UDP grants are returned earlier, with bounded stream/aggregate/queue headroom tuned from measurements.
- Scope: the adapter is still in-process and non-authoritative; Docker diagnostics do not qualify dedicated-host NFRs or freeze D-04.
- timestamp: 2026-11-04 — Full Engine race tests passed after UDP aggregate reserve and test-only event-cap changes.

## Eliminated

- hypothesis: the one-million event cap alone explains the 60-second failure.
  evidence: the 10-million bounded override removed capacity-exhaustion logs, but mismatch, UDP loss, target timeout, and stream reset remained.
