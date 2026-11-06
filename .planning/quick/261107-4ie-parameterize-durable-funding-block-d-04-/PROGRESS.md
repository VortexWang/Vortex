# Durable D-04 scan progress

- Implementation commit: `34da54dbcf3fa905abb03cec0f5ed5b463b28c25`.
- First native run: `/root/Vortex-phase4-current/artifacts/phase4/d04-durable-scan-34da54d` on the authorized Ubuntu host. Terminal status `fail`: all 18 cases at 64/256 KiB passed; all 9 cases at 1024 KiB failed because the fixture's four-second initial lease expired during the larger initial workload. Original logs remain intact.
- Correction commit: `20372514f193f7ebab98009f8701fd870b95c506`. Candidate-only initial TTL is 30 seconds with a 90-second context per transport. Default regression TTL stays four seconds. Issued Hub credit is reported as outstanding; the conservation residual is no longer mislabeled as occupancy.
- Second native run: `/root/Vortex-phase4-current/artifacts/phase4/d04-durable-scan-2037251-retry`, process PID 195244, runner log `/tmp/d04-durable-2037251-retry.log`. Confirmed live immediately after launch. Inspect live process and summaries before continuing; do not restart solely because the local SSH handle has ended.
- An aborted stale-source attempt also exists at `d04-durable-scan-2037251`; its name is not source identity. Do not use it as evidence for 2037251; inspect its manifest.

## Evidence limits still open

This is a finite, in-process Agent/Hub fixture with SQLite persistence and authenticated control traffic. Its target byte floor equals its maximum (twice the block size), so changing target seconds does not exercise a changing consumption-derived target. It does not establish the required 8 MiB floor, 16-stream mixed-load throughput, issuer wait percentiles, merge rate, or grant idle-return proof. Block-tail return is distinct from issuer credit release. A complete passing matrix cannot alone lock D-04 or close Phase 4. The original P4-Q4 criteria in `04-CLOSURE.md` remain authoritative.

Validation so far: 27 Python tests passed; default funding-block suite passed at 34da54d; native corrected matrix pending. No Phase 4 completion claim.
