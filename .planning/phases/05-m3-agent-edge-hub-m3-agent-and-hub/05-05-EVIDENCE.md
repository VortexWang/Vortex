# Phase 5 Plan 05-05 Evidence

## Result

All required checks passed for implementation commit `a1ab77c637f1a2c523f9d94a6d16db3d2bc0676a`. Native macOS arm64, Docker Linux arm64, and GitHub Actions Ubuntu 24.04 amd64 ran `make phase5-wave2-check` on that same clean source commit. The Actions job ran both Wave 1 and Wave 2 and completed successfully.

## Contract Approval

- Contract: `vortex.control.v1` 1.1.0.
- Owner-approved proposal SHA-256: `d166443ec3b6e92180592f7ef6354306169f69fd930c459e7ac869cdac3d9e7218`.
- Approval time: `2026-11-05T11:26:21Z`.
- The separately drifted VLink 1.0.0 hash is outside this approval and was not changed by this plan.
- `tools/phase5/contracts_check.sh` passed reproducible Protobuf generation, the control frozen-hash check, requirement mapping, contract unit tests, and SemVer change validation.

## Same-SHA Validation

| Host | Environment | Result | Raw evidence |
| --- | --- | --- | --- |
| Native | macOS arm64, Go 1.27.3 | Passed | [native evidence](../../../artifacts/phase5/wave2/a1ab77c/native/report.json) |
| Docker | Linux arm64, `golang:1.27.3`, Debian 13 | Passed | [Docker evidence](../../../artifacts/phase5/wave2/a1ab77c/docker/report.json) |
| GitHub Actions | Ubuntu 24.04 amd64, run `42026504229` | Passed | [Actions evidence](../../../artifacts/phase5/wave2/a1ab77c/actions.json) |

Each local report records the same full commit SHA. The reports include full Go test and vet logs, Engine runtime logs, contract check logs, Go version, architecture and status. The Docker checkout and native checkout were clean before the command ran.

## Implementation Coverage

- Control contract 1.1.0 is approved, versioned, generated and mapped; the CI gate detects unreviewed contract drift.
- Control config updates validate authenticated node identity, schema, transaction ID, payload digest, expected version and fencing generation before durable reconciliation and Engine Prepare/Commit/Abort. SQLite retains the result and `activation-unknown` state across reopen/replay.
- Upgrade artifacts are digest/signature checked, staged on the target filesystem, compatibility and disk checked, drained to a deadline, atomically activated and health checked. Durable recovery preserves unknown activation; health failure restores compatible state and the previous release unless rollback is blocked by an irreversible migration. Tests cover preflight rejection, staging retry, repeated upgrades, drain timeout, activation uncertainty, healthy restart and rollback crash boundaries.

## Scope Limits

- The test Hub remains a protocol fixture, not a production enrollment or revocation authority.
- `EngineApplier` reaches the process-local Engine transaction manager boundary. This plan does not claim that the full configuration payload is hot-applied to business routes.
- Node service/systemd orchestration for invoking the installer remains a deployment integration item; the file-backed installer lifecycle itself is implemented and tested.
- Phase 4 performance qualification remains on its separate branch and is not inferred from these checks.
