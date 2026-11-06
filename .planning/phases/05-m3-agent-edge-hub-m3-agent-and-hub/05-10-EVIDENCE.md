---
phase: 05
plan: '10'
status: verified
plan_complete: true
updated: 2026-11-06
---

# 05-10 operational node evidence

Product source: `061ffb5577a726b0431d2de9697850d86be061ef`.
Evidence root: `artifacts/phase5/check/061ffb5577a726b0431d2de9697850d86be061ef/`.
`pair.json` binds both hosts, `files.sha256` covers retained raw outputs.

## Actual behavior

- `runCommand` exposes enroll/run/status/drain; legacy no-subcommand invocation emits deprecation. Enrollment uses a local CSR and private token file/environment, rejects a second writer before opening SQLite and never echoes tokens.
- `LockProcess` uses nonblocking exclusive flock, O_NOFOLLOW, owner/mode/type/link-count checks. Run/enroll take it before Store.Open. Status/drain use IPC, never another database writer.
- `ServeAdmin` and AdminCall require owned mode0700 directory and mode0600 Unix socket, bounded paths, requests, deadlines and eight workers. Strict request decoding rejects duplicate/unknown keys and trailing JSON. Live socket replacement is refused; stale owned sockets are recovered.
- Formal Agent/Engine BeginDrain/Drain/Stats: admission and drain share a lock. New TCP/UDP OPEN on existing muxes is rejected. Existing TCP completes response and half-close during grace. Forced drain closes tracked links and joins stream cleanup; deadline never proves financial release.
- Actual run SIGTERM invokes bounded drain then Stop. The no-Hub compatibility branch now also observes the external signal context, avoiding an indefinite detached-context wait.
- Non-root systemd unit uses private state/runtime directories, bounded ExecStop, Restart=on-failure and50s stop allowance. Scratch image executes the actual binary as PID1 UID65532; approved pinned builder only, no dependency or normative contract change.

## Same-source verification

| Host | Gate | Result |
|---|---|---|
| Clean checkout, OrbStack Linux arm64 | make phase5-check | passed |
| Clean checkout, local Docker arm64 | node_deployment_check.sh | passed |
| Ubuntu24.04 amd64 Actions42186204143 | full workflow, phase5-check, node_deployment_check.sh | passed |

Actions: https://github.com/VortexWang/Vortex/actions/runs/42186204143

Both hosts retain explicit PASS for `TestNodeCommandsDrain/commands-grace`, `/commands-force`, `/commands-restart` and `TestNodeContainerLifecycle`, no required skips. Force includes live TCP and UDP. Each command scenario independently checks the original unresolved512-byte liability and origin boot after lifecycle changes. Second run cannot change the current boot. Repeated drain is idempotent.

Container fixture uses a real named volume, checks configured non-root identity, clean SIGTERM exit0, SIGKILL/restart with fresh boots, and reads unresolved512-byte debt after copying the stopped durable store. It tests deployment supervision; actual business traffic drain is verified separately by the real authenticated mux/node scenarios. Both Linux architectures compile; arm64 and amd64 runtime results come from their native hosts, not cross-build inference. Binary hashes are retained; binaries are generated outputs, not committed evidence.

Ubuntu systemd-analyze verify passed in an isolated root with the actual node binary and minimal implicit target fixtures. This validates unit/command configuration, not an actual systemd boot or end-to-end upgrade. macOS has no systemd; its unavailable result remains explicit.

Both complete gates preserve prior contract, real regional/renewal/identity process, race, full-disk and recovery regressions. Raw Linux audits were rerun independently:5 recovery snapshots/10 faults,12 renewal-safety snapshots and retention/source-conservation checks passed on both sets. macOS Python SQLite could not reopen these Linux snapshots; that attempt is not counted as passed, and pinned Linux audits provide the results.

Native development root tests, Agent/CLI and Engine runtime race, vet passed before the committed pair. Development-only long Unix socket failures led to a short per-database default path and were not treated as acceptance evidence.

Actions artifact13488490998 archive hash and selected member list are retained in `actions/artifact-selection.json`:112 current gate/deployment members selected,2229 historical members omitted. Existing historical artifact directories are not evidence for this source.

## Scope and next boundary

T-05-17/18 are covered by private IPC, duplicate-writer rejection, real drain/restart and durable liability assertions. D-03/04/06/07/16/17/18 remain intact. No Engine SQL dependency, backend business traffic or authority rollback added. Actual Agent-managed Engine remains the static destination runtime; Hub is the protocol test adapter. No performance qualification or production backend claim.

Only05-10 is complete. G05-02 remains open for actual signed service upgrade/rollback in05-11;05-12..14 and phase-wide verification/UAT remain outstanding.
