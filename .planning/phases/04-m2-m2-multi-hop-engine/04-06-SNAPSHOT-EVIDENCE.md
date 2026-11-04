# Phase 4 Plan 04-06: Immutable Billing Snapshot Evidence

Date: 2026-11-04

This execution slice adds a process-local `CreditBook` for a static, startup-pinned route. Each pinned snapshot has its own finite raw-byte gate and immutable rule, execution node, path generation, policy revision, and billing coefficient. The book preallocates the ceiling of each snapshot budget before startup; selecting a snapshot changes only the active admission gate for new sessions.

Existing sessions retain the gate selected at admission. A SIGHUP reload is accepted only when the complete pinned configuration is unchanged and `active` plus a strictly increasing `selection_revision` are the only differences. Unknown or late grant outcomes remain in the original snapshot gate and cannot be applied using another snapshot's coefficient or funding.

Validation performed:

- `go test -race ./runtime/accounting ./runtime/config ./cmd/vortex-relay`
- `go test ./...` and `go vet ./...`
- native five-process TCP topology: `tools/phase4/snapshots.py`
- Docker Compose five-process TCP topology: `BACKEND=docker python3 tools/phase4/snapshots.py`

Both topology runs passed the same semantic workload: an existing TCP session exchanged payload before and after selection; a new session exchanged payload after selection; final state showed old usage in `old`, new usage in `new`, active revision 2, and zero sessions/held bytes. The fixture confirms static-route session affinity and billing snapshot isolation. It does not qualify path switching, a durable authority, D-04, or full NFR-044.

The runtime snapshot evidence is in `artifacts/phase4/snapshots/native` and `artifacts/phase4/snapshots/docker` in the local worktree. GitHub Actions is wired to run `make phase4-runtime-snapshots` on the same workflow path; its result remains pending until the clean commit's run completes.
