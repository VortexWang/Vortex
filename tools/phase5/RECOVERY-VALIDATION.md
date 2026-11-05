# Phase 5 recovery validation

Run `sh tools/phase5/docker_check.sh` from the committed repository. It creates
an isolated clean checkout of HEAD, uses pinned Go 1.27.3 and checksum-verified
protoc 36.2, installs the locked Go generators, and runs `make phase5-check`.
The full disk fixture is a separate 4 MiB tmpfs; it never fills the host disk.
Raw logs, SQLite snapshots, SHA-256 manifests and verdicts are saved under
`artifacts/phase5/check/<full-SHA>/docker`. Actions runs the same Make target
and publishes `phase5-recovery-<full-SHA>`.

The gate requires Linux, the isolated full-disk directory and clean source. It
fails if any required fault case, ledger snapshot or independent audit evidence
is absent. Old artifact directories cannot substitute for a current run.

Validated runtime scope is the Agent-owned static destination Engine. A
RuntimeConfigUpdate must exactly match that rule, target, peer and listener;
unsupported options fail before Prepare. The listener may establish TLS before
activation, but new streams are denied until matching current-boot runtime and
share leases, funded credit and trusted time permit Commit. Prepared requests
are retried after authenticated authorization arrives. Headless control without
an Engine cannot report successful execution.

On restart, old authorizations become settlement-only and unfinished writes
become typed unknown liabilities. Config holds are released only after a retained
same-instance Engine query or a same-kernel/same-PID-namespace process witness
proves inactivity. PID reuse is checked against `/proc/<pid>/stat` start ticks.
Namespace changes, missing witness, malformed evidence, permissions failures and
new boot IDs alone never prove termination. Operators must resolve unproven
execution at the existing authorized control boundary; reconnect is not proof.
A reissued config uses a new transaction, higher fencing generation and current
boot grants; prior unknown financial liability remains held.

Hub S/R recovery and the durable backend adapter in tests are explicitly
**protocol-test-only**. No result certifies production backend allocation,
production deployment or Phase 6 completion. The raw ledger includes source
allocation/issued/R totals, S, unique transfer, continuous tail and hash-bound
confirmation checkpoint. No confirmation deletes unsettled or disputed records.
