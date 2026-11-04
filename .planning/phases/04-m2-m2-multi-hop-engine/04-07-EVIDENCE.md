---
phase: 04-m2-m2-multi-hop-engine
plan: 07
status: verified
source_sha: a7a7432b11b5c3c7801e7fcfe3fe1664fe6c5b4e
---

# Plan 04-07 Evidence — Fail-Closed Source Authorization

Date: 2026-11-05

## Result

G-04-01 is closed for the default runtime boundary. A source relay without an
explicit `qualification_only` test mode or configured source authorization
fails configuration/startup. The relay bridge rejects a missing Gate instead
of forwarding unmetered payload. Default tracer policy no longer synthesizes
runtime, segment, rate-share, connection-share, or billing authorization.
Qualification fixtures explicitly opt in, and emitted metadata identifies
that boundary.

This is not a production authorization or durable accounting implementation.
The qualification adapter is process-local; T-04-02 and NFR-044 remain open
for authoritative grants, durable settlement, and complete per-source
conservation. No Phase 4 or performance qualification is claimed.

## Same-SHA Verification

Source SHA: `a7a7432b11b5c3c7801e7fcfe3fe1664fe6c5b4e` (`a7a7432`). The commit
was clean on both hosts.

| Host | Verification | Result | Boundary |
| --- | --- | --- | --- |
| Local OrbStack, aarch64 | `DOCKER_CONTEXT=orbstack make phase4-m2-check` | Pass; engine race, targeted Phase 4 regression, and Docker VLink checks | Correctness regression only; performance is `environment-limited` |
| Local OrbStack, aarch64 | `BACKEND=docker QUALIFICATION_TCP_HOST_PORT=19080 make phase4-runtime-smoke` | Five-role, three-relay TCP/UDP smoke passed; TCP and UDP payload counters reconciled, UDP 52,055/52,055 with 0 loss | `qualification-only`, `not-qualified`; port 19080 avoids an unrelated host listener on 18080 |
| GitHub Actions, `ubuntu-24.04` | [Run 41887199379](https://github.com/VortexWang/Vortex/actions/runs/41887199379) | Success, 12m16s; all steps passed | Shared runner regression evidence, not dedicated-host performance qualification |

The Actions run executed `phase4-m2-check`, finite source credit/exhaustion,
immutable snapshot selection, benchmark smoke, independent TLS/latency/fault
checks, mixed Compose workload, 16-stream/60-second accounting regression,
pool/mux/auto mode regression, and authorized route-switch/restart recovery.
Its `phase4-evidence` artifact includes host metadata, manifests, raw runtime
and TLS measurements, accounting events, mode results, fault results, and
checksums. The workflow emitted a Node.js 20 deprecation annotation for the
upload action; the job itself succeeded.

The same-SHA results prove the tested finite runtime and default authorization
regressions only. They do not establish dedicated Linux resource isolation,
formal NFR-003/005/006/007/039/040/044 qualification, D-04 parameter approval,
or durable Agent/Hub authority. Those items remain open in
`04-06-QUALIFICATION-MATRIX.md`.
