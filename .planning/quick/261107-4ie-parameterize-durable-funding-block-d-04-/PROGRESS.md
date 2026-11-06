# Durable D-04 scan progress

- Implementation commit: `34da54dbcf3fa905abb03cec0f5ed5b463b28c25`.
- First native run: `/root/Vortex-phase4-current/artifacts/phase4/d04-durable-scan-34da54d` on the authorized Ubuntu host. Terminal status `fail`: all 18 cases at 64/256 KiB passed; all 9 cases at 1024 KiB failed because the fixture's four-second initial lease expired during the larger initial workload. Original logs remain intact.
- Correction commit: `20372514f193f7ebab98009f8701fd870b95c506`. Candidate-only initial TTL is 30 seconds with a 90-second context per transport. Default regression TTL stays four seconds. Issued Hub credit is reported as outstanding; the conservation residual is no longer mislabeled as occupancy.
- Second native run: `/root/Vortex-phase4-current/artifacts/phase4/d04-durable-scan-2037251-retry`, runner log `/tmp/d04-durable-2037251-retry.log`. Stopped after three recorded failures: a 30-second initial lease wait also exceeded BackendLossGrace, leaving backend ledger delivery incomplete. PID 195244 was explicitly terminated; no scan/test process remained at the subsequent inspection.
- Follow-up fixture fix refreshes the simulated online backend heartbeat every five seconds during the lease wait. A post-grace heartbeat was correctly rejected before this fix. The largest 1 MiB / 20% TCP case now passes locally in 43.213s (`/tmp/d04-heartbeat.JBjNk6`). No production autonomy rule changed.
- Dynamic Engine/tracer runner now supports `--full-factorial` (27 combinations), preserving seven-case default behavior. All 28 Python tests pass. A new scan will require its own clean commit and evidence root.
- An aborted stale-source attempt also exists at `d04-durable-scan-2037251`; its name is not source identity. Do not use it as evidence for 2037251; inspect its manifest.

## Evidence limits still open

This is a finite, in-process Agent/Hub fixture with SQLite persistence and authenticated control traffic. Its target byte floor equals its maximum (twice the block size), so changing target seconds does not exercise a changing consumption-derived target. It does not establish the required 8 MiB floor, 16-stream mixed-load throughput, issuer wait percentiles, merge rate, or grant idle-return proof. Block-tail return is distinct from issuer credit release. A complete passing matrix cannot alone lock D-04 or close Phase 4. The original P4-Q4 criteria in `04-CLOSURE.md` remain authoritative.

Validation so far: 27 Python tests passed; default funding-block suite passed at 34da54d; native corrected matrix pending. No Phase 4 completion claim.

## Current native run — 2543fb7

The corrected native run uses exact commit `2543fb745d942ddf710c02d502a3675b9b3c7fe9`. Sequential wrapper PID 196261 is `/tmp/vortex-2543fb7-scans.sh`; its log is `/tmp/vortex-2543fb7-scans.log`. First it runs 27 durable cases into `artifacts/phase4/d04-durable-2543fb7`, preserving SQLite ledger copies. Then it runs `d04_run.py --full-factorial --duration 15 --repeats 3 --rate-mbps 100` into `artifacts/phase4/d04-factorial-2543fb7`, with GOMAXPROCS=1 and 65 seconds idle per trial. The wrapper restores the original UDP buffer limits via EXIT trap after the performance scan. Both scripts keep locked=false; inspection and parameter review remain required. Do not checkout a new SHA in the remote worktree while these scans are active.

Local corrected validation: largest 1 MiB/20% TCP candidate passed; `go vet ./agent` passed; all 28 Python tests passed. Full native results remain pending. For monitoring, inspect PID 196261, its child processes, and the log/summary files rather than restarting when an SSH handle disappears.
