---
phase: 05
plan: '14'
status: complete
plan_complete: true
updated: 2026-11-06
---

# 05-14 consolidated acceptance evidence

The full phase gate invokes gap_check.sh after all existing regressions. It reruns the raw recovery, policy, renewal and settled-retention auditors, verifies 31 named cases across seven process workflows, hashes nonempty files and every retained raw SQLite snapshot, binds the result to committed clean source and pinned Go 1.27.3, and records frozen contract/governance digests. Missing, skipped, malformed or changed evidence blocks the final report. Exact source SHA is checked against collector HEAD; unrelated dirty user files are excluded only by clean Docker checkout and Actions, never relabeled clean in the development worktree.

Eleven Python evidence verifier tests passed natively, including a WAL snapshot regression using immutable read-only SQLite access, and rejection of absent audit files, wrong SHA/hash, skipped/empty/unhashed process logs, forged SQLite and incomplete case/snapshot manifests. Native actual process suite passed; Darwin service-upgrade process case explicitly skipped because Linux process witness is required, so native result is development-only. Required Linux gate rejects skipped cases. Real process logs retain process IDs/binary hashes and active node boot/path generation without private credentials.

## Same-SHA paired gate

- Source: `deb8b08e5cc8ee4e6f77bc700a4b5b36836c68c1`.
- Docker: clean checkout, fixed `golang:1.27.3` image digest, 11 evidence tests, 31 process cases, 60 snapshots, report `passed`.
- GitHub Actions: run `42224211933`, Ubuntu 24.04 amd64, Go 1.27.3, full Phase 5 workflow and deployment lifecycle, report `success`.
- Independent re-audit: `validate_manifest` passed for both hosts; both reports retain `backend=protocol-test-only` and `production_backend_authority=false`.
- Action artifacts were downloaded and checked against the manifest; no skipped required case, missing audit, changed hash or forged SQLite snapshot was accepted.

The 05-13 `bc1c262` evidence remains historical and is not reused as final 05-14 evidence. No normative contract/dependency or Phase 4 qualification changes were introduced. Production backend authority, physical power-loss guarantees, systemd daemon boot and Phase 4 performance qualification remain outside this phase slice.
