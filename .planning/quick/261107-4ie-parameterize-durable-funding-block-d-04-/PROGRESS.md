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

## Independent closed-ledger audit

`tools/phase4/audit_d04_ledgers.py` audits downloaded SQLite copies independently of the Go test exit status and summary booleans. The first corrected native case passes for both transports: 52 usage batches each; 237568 issued, 212992 committed, 24576 remaining. Agent/Hub/backend batches match by node ID, batch ID and payload; every payload digest and decompressed file digest matches; SQLite integrity checks pass. Agent source ownership, grant balances and Hub delegation conserve bytes. There are no pending demands or held/unknown usage. A copied summary with committed_bytes incremented by one is rejected with `summary differs from SQLite`. This is finite fixture evidence, not dynamic performance or production authority acceptance.

## Same-SHA Docker diagnostic

Exact detached verification worktree `/private/tmp/vortex-phase4-2543fb7-verification` ran the 2543fb7 Docker workload with GOMAXPROCS=1, 16 TCP streams, 15 seconds per phase, and PHASE4_FORMAL=1. Evidence root: `artifacts/phase4/qualification/docker-current` in that worktree. Host and report both show dirty=false and exact SHA 2543fb745d942ddf710c02d502a3675b9b3c7fe9. Runtime correctness passed; mixed TCP 3300.647845 Mbps, UDP 99.95968 Mbps and zero loss; each direction reconciled 13066165956 bytes. Source used 26132331912 bytes, with pending/unknown/held/session/denial all zero. This is OrbStack diagnostic evidence; report performance_qualification remains not-qualified.

- host.json SHA256: `47d4c5c34a8f17d57f9d815518d33ad79bd29de040f4466c656eb3b18a0de5a2`
- report.json SHA256: `68b5082711fc49b397e131eedb198fcd31776648822c6907be19921eadfd7ca1`
- source credit SHA256: `de4b74fc76204b6914b8c70a587457dcb5735600a9b0566672cdcaf76315f886`

Local Engine `go test -race ./...` passed (cached where applicable). Root `go test ./...; go vet ./...` is still running in local session 72562; inspect its result before claiming a pass. No Actions run for 2543fb7 has been established.

Root session 72562 subsequently completed successfully: all root module tests passed (agent 202.223s), followed by go vet exit zero. New `audit_d04_timeline.py` independently checks every sampled source-conservation equation, stable snapshot/budget, nondecreasing used bytes, bounded dynamic target and (when requested) a separately recorded 65-second idle window, observed target growth/shrink and final settlement. Its negative tests reject corrupt interior samples and missing idle/sampling evidence; all 31 Phase 4 Python tests pass. This auditor runs after the existing remote scan and does not alter its source SHA or hot path. It does not prove issuer credit return.

## CI and full-matrix audit preparation

The authenticated CLI account nodeox has READ access to VortexWang/Vortex and ADMIN access to its existing public nodeox/VORTEX repository. Isolated branches codex/phase4-2543fb7-ci-base (9654f66) and codex/phase4-2543fb7-ci (2543fb7) were pushed there, with draft PR https://github.com/nodeox/VORTEX/pull/1 for CI evidence. Its main branch was not changed. Phase 4 run 42307365335 started; a run in progress is not a pass. Default pull-request checkout may record a merge SHA, so actual checkout identity must be inspected before claiming exact SHA equivalence.

The first eleven corrected durable cases were downloaded and independently reconciled successfully. A separate full-factorial auditor now recomputes all 81 dynamic trial results from raw files, verifies exact generated configuration and semantic digests, each configured factor, workload coverage, sampled conservation and the independently timed idle window. It leaves locked=false and does not claim production issuer qualification. Actual 81-trial audit remains pending.

## CI failures investigated

Phase 1 run 42307365328 failed its frozen VLink hash check. The recorded approved hash dbf1607d binds the original draft; subsequent Phase 3 contract edits were already called out as outside approval in 05-05-CONTRACT-PROPOSAL.md. Do not fabricate owner approval or silently reset the hash. Phase 3 run 42307365481 passed VLink tests and the benchmark executable but failed the shell metric parser: structured UDP logs split benchmark labels from numeric records. The parser now associates continuation metrics with the last benchmark label. All seven metric series are recovered with three samples from the original CI log, and a fresh local benchmark run exits zero with benchmark_status passed. No measurement or numeric criterion was changed.

Phase 4 PR checkout is now explicitly pinned to pull_request.head.sha (or github.sha for pushes), avoiding synthetic merge identity in future exact-source evidence. The current remote worktree remains unchanged at 2543fb7 while scans run.

## Exact-source 0c2ba71 regression and warmup provenance

Clean commit 0c2ba71c0653ca1c806dc3df1db93d847544011b changes CI/measurement parsing and independent auditors only; `git diff edb1849c..0c2ba71 -- engine` is empty. Docker mixed regression in /private/tmp/vortex-phase4-0c2ba71-verification passed with 16 TCP streams, 15 seconds per phase, mixed TCP 3308.737583 Mbps, UDP 99.97632 Mbps and zero loss. Target/accounting each direction 12982180004 bytes, source used 25964360008 with pending/unknown/held/session/denial zero. Host SHA256 82a84c59db4d6a4b05632f86fc16f6aacf4699112ea5d89159a98d93079a8a56; report 6b0ac36700db520909a27e91f1b322437cf4085722b6edbc4bd20cb5dfe1ff02; credit 5f535d81c1c938f933d4501e9fcf0341c27646382d82b6ceb8d0fed775bac5bb. Performance remains not-qualified for this short OrbStack run.

Phase 4 Actions run 42307365335 passed and its full artifact was downloaded to artifacts/phase4/ci-2543fb7/phase4. It checked out merge 86573b9e26983275a2cf37da2a597ea399efa598, whose tree 5e91e5fc04fcee87e57334262ff5cfa48a90e7b4 exactly equals 2543fb7's tree; this is content-identical regression evidence, not an exact commit match. New Phase 4 run 42308608919 checks out 0c2ba71 directly and is still running. Phase 3 run 42308609071 passed after parser correction.

Remote /root/Vortex-phase4-final-0c2ba71 is a separate clean worktree, prepared without touching the running matrix. Native preflight passed (Ubuntu24.04, native x86_64, 12CPU,31GiB,Go1.27.3,enp3s0f0). No formal workload was started concurrently with the matrix.

An independent provenance inspection found that the older edb1849c formal directory contains both warmup (60s) and warmup300 (300s). Only warmup300 has the exact semantic digest f1fae018... shared by the formal trials. The saved gate says warmup_seconds=60 but does not record its input paths; do not describe the 60s report as its hash-matching input. New final formal evidence must identify the actual warmup report and duration explicitly. The current gate includes duration in semantic identity; a 300s warmup can meet the >=60s floor while maintaining exact manifest identity.

## Durable matrix complete; dynamic matrix running

Corrected native durable matrix: 27/27 cases passed, both TCP and UDP. All 54 closed Agent/Hub/backend SQLite sets also passed the independent auditor. Scan summary SHA256 2227e52603dda9599114fe2c4868e26348d28c9f127ba69a4254229bf3bb4f2e; independent-audit.json SHA256 3eb8a17025a1c46f771916d16024e8a009a6b0fa1c57ab9beaa019515defbd73. These remain finite fixed-target safety tests. Dynamic full-factorial matrix started automatically after durable_exit=0; PID196261 continues unchanged.

Exact-head Phase 4 Actions run https://github.com/nodeox/VORTEX/actions/runs/42308608919 passed. Downloaded evidence and job metadata are at artifacts/phase4/ci-0c2ba71; all 15 host reports bind clean 0c2ba71c0653ca1c806dc3df1db93d847544011b. This includes native/Docker comparison, 16-stream60s accounting, explicit modes, credit/snapshots and recovery.

First dynamic trial passed independent audit of 103 sampled conservation equations, 65s idle window and target growth/shrink: TCP654.678696Mbps,UDP99.96672Mbps,zero loss. The first combination's three repetitions also passed. Local automatic evidence monitor runs in exec session74598, downloading JSON/config evidence and auditing each completed trial; remote raw events remain intact. Full-factorial final audit remains pending.

The timeline auditor now optionally recomputes the exact consumption-derived target during cold-start elapsed<58s, before any rolling-minute bucket can expire. First trial: 58 formula samples match, including three uncapped growth samples. Negative tests reject the wrong horizon even when target bounds still hold. All32Phase4Python tests pass. This is independent auditor enhancement; it does not change running workload source or credit semantics.
