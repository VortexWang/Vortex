# 05-07 regional runtime evidence

Status: **plan complete; Phase5 gaps remain open**. Final implementation SHA `ec155ad92d5a6d940fc0f22a051872fb0d982c60`.

## Actual integration

- `cmd/vortex-node/main.go` calls `ControlClient.RunRegion`; `--control-config` consumes the approved runtime envelope, optional --region must agree. --hub becomes one configured entry with Hub UUID and certificate name. No competing single-address loop.
- `agent/regional_control.go:RunRegion` filters explicit same-region candidates, bounds TLS/readiness, probes online candidates before restricted fallback, uses bounded jitter/retry and joins canceled receiver/connection ownership. Healthy lifetime does not consume next failover sweep budget. Failed handshakes do not reset healthy backoff.
- `agent/control.go:connect` negotiates fresh session/region/status, serializes sends, enforces 1MiB transport cap and unknown-field rejection, cancels silent status watchdog, denies restricted new grants/config. `beginSession` cancels a blocked old sender before acquiring the mutation boundary; stale streams cannot install grants/config/clock/revocation. Persistent sequence exhaustion stops before MaxInt64 overflow.
- `hub/status.go` and `agent/store/control_status.go` report already persisted epoch/heartbeat/deadline; empty/untrusted/expired state reports blocked. Status never creates backend time or balance. Original boundaries survive reopening. Current status and fixed aggregate counters expose no tenant/rule/stream labels.
- Protocol-test Hub entry exposes deliberate stall faults and trusted offline allocation/config fixtures; these are not production backend authority. The TCP process fixture uses the existing separate Engine qualification identity; shared production identity rotation remains05-09.

## Executed checks

| Host/check | Result |
|---|---|
| Local macOS arm64 root `go test ./...`, Agent/Hub race and vet | passed |
| Local targeted `go test -count=10 -race ./agent -run TestRegional` | passed; final pair repeats every required case |
| Final clean Docker Linux arm64 `make phase5-check` on ec155ad | passed |
| Actions Ubuntu24.04 amd64 same SHA full gate | passed, [run42154985414](https://github.com/VortexWang/Vortex/actions/runs/42154985414) |
| Independent Linux raw SQLite re-audit, each host | passed; five snapshots/ten existing faults each |

Each final host ran `TestRegionalProcessFailover` with actual built vortex-node and vortex-test-hub binaries: silent initial stream, silent established stream, autonomous first candidate, recovering first candidate, and TCP under original authorization across actual Hub termination. TCP keeps one target connection, exact payload, unchanged original expiry, conserved source funds and a real 512-byte unresolved funded reservation. Other process cases retain old-boot512-byte unknown debt on both stores and terminate the node cleanly.

Required regional race cases ran ten times on both hosts: hostile/mutated/unknown/overflow/stale status; stale incarnation mutation; blackholed TLS and losing transport cleanup; stalled stream; restricted-mode online preference; delayed authorization/config after successor ownership; revoked peer/no healthy candidate; blocked mutation cancellation; healthy lifetime exceeding candidate budget. Nonempty named-test checks in tools/phase5/check.sh reject missing/skipped scenarios. Root/Engine/race/full-disk/SIGKILL source conservation regressions also passed.

Raw logs, host metadata, ledger snapshots, pair metadata and file SHA256 inventory: `artifacts/phase5/check/ec155ad92d5a6d940fc0f22a051872fb0d982c60/`. Current Actions root copied; historical nested artifacts excluded. Both raw recovery sets independently re-audited in pinned Linux Go1.27.3 image.

## Failures and fixes retained

Initial0535c6e Docker/Actions42154056514 failed a race fixture whose50ms dial/300ms total budget could expire before certificate verification under Linux race; attempts=0 correctly failed the assertion. Revised test starts a bounded1.2s attempt window after FULL/key setup, uses500ms dial, and still requires actual rejection attempts. dd109e5 then passed both hosts (Actions42154489516); final ec155ad additionally fixes and regression-tests healthy-stream loss resetting the next candidate budget. Initial failed Docker logs remain under artifacts/phase5/check/0535c6e494a239718cf83754ff1d0f163b98efdb; failed results were not relabeled passed.

## Acceptance/threat disposition

T-05-11 identity/session/source boundary and T-05-12 cancellation/debt boundary are covered by the named process/race tests plus raw recovery audit. No new library or normative contract change. Added Store read-only snapshot and sequence-exhaustion tests, operational runbook and expanded existing gate (plan deviation: necessary integration files beyond listed paths).

Only the regional-control portion of G05-01 is complete. Probe/health-version reporting, remote durable renewal and retention remain05-08; G05-02/03/05 remain open. Credential revocation test rejects current TLS peer, but full shared control/VLink propagation is not claimed. CurrentHubStatus is observation, never authenticated allocation/ownership proof. Existing admitted TCP continues only within original valid authority; unknown debt never released. No Phase4 qualification or Phase6 backend closure inferred. No full requirement marked complete while dependent shared plans remain unfinished.
