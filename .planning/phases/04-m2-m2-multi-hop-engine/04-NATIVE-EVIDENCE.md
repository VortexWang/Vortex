# Phase 4 Native Linux Evidence

## Runtime topology qualification

- Commit: `86f06b3fc9a166a766c8c17a6b5df0ab038ba99f` (clean worktree)
- Host: Ubuntu 24.04.5 LTS, Linux 6.8.0-142-generic, x86_64
- CPU / memory: 4 vCPU, 7894 MiB; AMD EPYC 7713 host CPU
- Runtime command: build `engine/cmd/vortex-relay`, then run `BACKEND=native VORTEX_RELAY_BIN=/tmp/vortex-relay-86f06b3 QUALIFICATION_DIR=/tmp/vortex-phase4-qualification-86f06b3 ./tools/phase4/qualification.sh`
- Result: `qualification=topology-and-accounting-pass`, all five role logs contained `OPEN authorized`, exact TCP echo payload returned, and destination accounting JSONL recorded two committed 24-byte directions.
- Scope: functional runtime and accounting tracer smoke. The adapter is in-process and non-durable; this does not qualify production accounting durability.

## Runtime TCP and UDP smoke on the gap-closure commit

- Commit: `9ddb724d822ef0f67b2694565262776916826ea6` (clean worktree)
- Host: Ubuntu 24.04.5 LTS, Linux 6.8.0-142-generic, x86_64; 4 vCPU, 7894 MiB.
- Command: `BACKEND=native VORTEX_RELAY_BIN=/tmp/vortex-relay-9ddb724 QUALIFICATION_DIR=/tmp/vortex-phase4-9ddb724 ./tools/phase4/qualification.sh`
- Result: `qualification=tcp-udp-topology-and-accounting-pass`; TCP half-close echo and a UDP datagram both crossed three relays and returned exact payloads. All five role logs recorded OPEN authorization and none reached the violation threshold.
- Accounting reconciliation: four destination events committed exactly 28 bytes in each direction for UDP and 24 bytes in each direction for TCP; reserved and used bytes matched.
- Scope: one TCP flow and one UDP datagram are a functional smoke only. It is not sustained mixed load, queue/fairness measurement, same-path performance ratio, or NFR qualification.
- Docker Compose was not run on the local host because the OrbStack Docker socket was unavailable. The same Compose target still needs a live Docker run.

## Stream reset cleanup follow-up

- Commit: `eb2737e43efe16ce09f60a5af6bd5e27b777ac8f` (clean worktree)
- Native Ubuntu qualification repeated with the new binary and again returned
  `tcp-udp-topology-and-accounting-pass`, with four exact committed events
  (28/28 UDP bytes and 24/24 TCP bytes across both directions).
- Explicit local stream reset releases the local mux record after bridge errors
  and idle timeout. The reset lifecycle test passed 100 times under race
  detection, and the complete Engine race suite passed.
- This remains a single-flow smoke, not sustained mixed-load or NFR evidence.

## Benchmark smoke on the same host

- Command: `BACKEND=native VORTEX_RELAY_BIN=/tmp/vortex-relay-86f06b3 make phase4-m2-benchmark`
- VLink tracer library throughput: 722.37 MB/s forward, 711.69 MB/s reverse.
- TLS baseline: 1064.86 MB/s; VLink mux: 424.99 MB/s forward, 394.12 MB/s reverse.
- First-byte: mux median extra 238 us; prewarmed pool median extra 216 us.
- UDP loopback: 20,000 sent, 20,000 received, 0 lost at 1200-byte payload and 100 Mbps target.
- Verdict: benchmark smoke completed, but these are library/loopback measurements. No sustained workload traversed the three-relay runtime; no same-path ratio, concurrent TCP/UDP fairness, relay queue/drop reconciliation, or D-04 review was performed. NFR-003/005/006/007 and D-04 remain unqualified.

## Actions evidence and follow-up

- Actions run `41744803772` used the same clean commit and failed in `TestVLinkIntegrationUnauthorizedUDPDoesNotReachOpenOK`.
- Cause: when an OPEN rejection closed a stream after queuing OPEN_ERR, `Receive` could select the closed-stream signal first and return `ErrStreamState`, dropping the queued response.
- Fix: `MuxStream.Receive` now drains an already queued frame when the stream is closed. Local verification passed the focused test 100 times under race detection and `cd engine && go test -race ./...`.
- A new Actions run on the fix commit is required before accepting CI evidence.

