# Backend authority tracer

The independent `vortex/backend` module uses Go 1.27.3 (`GOTOOLCHAIN=local`),
pgx v5.11.0, Goose v3.29.0, sqlc v1.31.1 and the existing control 1.3.0 contract.
Exact dependency sums are in `go.sum`; tool hashes and upstream licenses are
recorded in `docs/governance/version-lock.yaml` and its closure review.

From the repository root, run `tools/phase6/tracer_real.sh` for development or
`tools/phase6/docker_check.sh` after committing for an exact-SHA clean clone.
Both start the pinned PostgreSQL 18.6 / Valkey 9.1.3 services, generate sqlc twice,
apply Goose explicitly, and execute normal and race tests with separate backend,
Agent and Hub processes. Docker source and evidence locations can be selected
with `PHASE6_SOURCE_ROOT` and `PHASE6_EVIDENCE_DIR`. Test services bind only to
localhost; the included password is exclusively for this disposable test setup.

Native tests need the exact Go installation in both PATH and GOROOT, plus
`PHASE6_DATABASE_URL=postgres://vortex:phase6-local-test-only@127.0.0.1:15436/vortex?sslmode=disable`
and `PHASE6_VALKEY_ADDR=127.0.0.1:16386`. Missing databases fail tests. Native
process tests build their own command binaries if none are supplied.

`vortex-backend migrate` is an explicit administration operation; `serve` never
migrates automatically. The local `mutate` / `operation` CLI requires explicit
actor/tenant scope. It is an administration interface, not the public REST API
or a substitute for the fixed user-role policy delivered by plan 06-08.

A mutation atomically writes desired state, operation, task, Outbox and immutable
audit. Workers lease with SKIP LOCKED and perform bounded external I/O after
commit. Matching receipt, task, operation and Outbox confirmation share one
transaction. Timeout, disconnect, expired lease and unmatched revision retain
unknown occupation. Confirmation alone never releases authority.

The process tracer applies a configuration with no listeners or rules through
the real Agent ControlClient, SQLite Reconciler and Engine transaction manager.
It proves control application and durable replay; it does not establish funded
runtime activation or business forwarding. Those remain behind the existing
Agent/Engine boundary and later orchestration/authority plans.

NodeControl and HubControl use distinct client-role trust roots. The Hub client
implements only session-bound authenticated backend heartbeat persistence and
HubStatus. A fresh Hub remains blocked with no signer. Unsupported authority
bodies fail closed. Full allocation, restriction, handoff and source-ledger
policy belongs to 06-05/06-06; the control contract is unchanged. The optional
Valkey projection contains only the PostgreSQL authority currently implemented;
later source balances must extend its loss/rebuild tests when introduced.

`serve` also runs the PostgreSQL rule compile consumer. The internal billing
authority producer calls `RuleService.BindCompileInput` with a validated immutable
snapshot and explicit signer/path/fencing generations. This pins the complete
canonical input to the task once; it does not publish artifacts. The consumer
reloads that exact input and atomically commits artifacts, fixed-member Prepare
tasks, Outbox confirmation and audit. Replays retain the same digest; a different
snapshot cannot replace a task's binding. Missing or unavailable billing snapshots
leave requests pending without artifacts or changes to prior active authority,
grants or unknown occupation. There is no default snapshot or latest-catalog
fallback. Formal funded snapshot production remains in 06-04/06-05.
