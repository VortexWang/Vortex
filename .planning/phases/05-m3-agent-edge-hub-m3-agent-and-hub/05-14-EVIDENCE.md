---
phase: 05
plan: '14'
status: incremental
plan_complete: false
updated: 2026-11-06
---

# 05-14 consolidated acceptance evidence

The full phase gate now invokes gap_check.sh after all existing regressions. It reruns the raw recovery, policy, renewal and settled-retention auditors, verifies31 named cases across seven process workflows, hashes nonempty files and every retained raw SQLite snapshot, binds the result to current committed clean source and pinned Go1.27.3, and records frozen contract/governance digests. Missing, skipped, malformed or changed evidence blocks the final report. Exact source SHA is checked against collector HEAD; unrelated dirty user files are excluded only by running clean Docker checkout and Actions, never relabeled clean in the development worktree.

Ten Python evidence verifier tests passed natively, covering complete fixtures and rejection of absent audit files, wrong SHA/hash, skipped/empty/unhashed process logs, forged SQLite and incomplete case/snapshot manifests. Native actual process suite passed; Darwin service-upgrade process case explicitly skipped because Linux process witness is required, so native result is development-only. Required Linux gate rejects skipped cases. Real process logs now retain process IDs/binary hashes and active node boot/path generation without private credentials.

05-13 sourcebc1c262 paired evidence remains historical and is not reused as the final05-14 source. Same-source Docker/Actions for this new gate, independent final audits, source review/security/Nyquist refresh and renewed owner UAT remain pending. No SUMMARY or phase completion claim yet. No normative contract/dependency or Phase4 qualification changes.
