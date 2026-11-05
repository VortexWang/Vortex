# 05-04 Incremental Evidence

Status: executing; no SUMMARY or completion claimed.

## Direct reconcile safety prerequisite

- Direct `ApplyConfig` previously omitted the immutable request/fencing transaction used by the control decoder. It could start another activation while an earlier commit remained unknown.
- Both entry paths now use `SaveConfigRequest` before Engine preparation. Direct calls persist their distinct request identity and fencing floor. Reusing a transaction with changed generation is rejected. Existing rows without matching fencing evidence cannot silently acquire it.
- Tests now require an unresolved commit to block subsequent activation. Reopen coverage proves the stored fence and uncertainty survive a new coordinator instance.
- Remaining: durable execution-query proof and resource-hold resolution, fresh-boot activation orchestration, atomic signer S/R transfer and prefix/tail checkpoint handling, process-fault and paired Docker/Actions final acceptance.

## Execution query and resource holds

- Added durable activation holds and append-only query observations (SQLite schema 10). Request/fencing persistence creates the hold before preparation.
- Reconciler recovery queries the Engine transaction, verifies ID and generation, and persists observation and disposition atomically. Missing state remains activation-unknown; matching active state recovers the lost result without releasing its resources. Only matching aborted/released state releases a non-active transaction. Legacy records without fencing cannot fabricate proof.
- Agent/store race tests and targeted lost-receipt, reopen, missing-state, wrong-generation and matching-abort tests pass locally. Final combined Docker/Actions coverage remains outstanding, along with S/R rotation and full boot recovery orchestration.

## S/R handoff and execution identity increment

- Schema 11 adds an append-only Hub ledger, unique epoch handoffs and per-delegation R transfer records. A FULL transaction applies revocations first, closes the prior issuer, seals S, moves only unissued R and opens exactly one successor epoch. The original allocation and all issued payloads remain immutable; path generation does not change.
- Hash-bound backend confirmations advance only over a contiguous prefix. Later tail records are independently replayed; confirmations delete neither history nor unknown liabilities. Hub recovery uses an explicitly protocol-test-only backend adapter boundary.
- Regression cases cover duplicate/concurrent handoff, restart, SIGKILL before/after commit, wrong ACK hash, out-of-order ACK, lost durable backend ACK plus new tail, and real bounded-filesystem exhaustion. The full-disk case requires the isolated Docker/Actions fixture.
- Activation queries now bind to the original Engine instance, preventing a new manager from recreating and aborting an old transaction to fabricate release proof. Reads of malformed persisted applied versions fail closed.
- Root tests and targeted Agent/Hub race pass locally. Clean-commit Docker and Actions results are recorded separately after execution; this increment is not full 05-04 acceptance.
- Still open: integrated fresh-boot re-Prepare/authority/Commit orchestration, authenticated successor-epoch acceptance at the Agent source-lineage boundary, complete recovery evidence gate and final plan verification. Existing source-lineage checks intentionally continue rejecting an epoch change without handoff evidence.

## Integrated recovery implementation (awaiting final clean-SHA pair)

- Authorization 1.1.0 approved in-session from exact proposal `185541ee09e8dfbb0333a6249e0571c805b182220c77e7c2a79976d45efa4b6e`. Frozen schema hash `0febf4b5b7f23cf17f6121d22e7aa41071a3728b2953e462737844f2091c7ece`; control remains 1.2.0. Agent authenticates the issuer lineage, records unique successor attestation and consumes at most transferred R. Prior grants/debts/path generation remain unchanged. Same-session renewal uses a new epoch only with committed parent handoff evidence.
- SQLite schema 13 stores execution identity, Linux process witness, append-only query/boot evidence, activation holds, Hub S/R records and Agent transfer budgets. Late stage results cannot mutate request version or regress applied_version; new received/prepared transactions block conflicting requests.
- Startup reports old activation records over existing NodeEvent, fences old boot authorizations to settlement-only, and converts unfinished reservations to typed unknown liabilities. Same-kernel/same-PID-namespace PID/start-tick death evidence permits configuration release; all unproven cases keep activation unknown. Financial unknown liability is never released by process death.
- A real RuntimeApplier validates the exact configured static destination/listener, stages Prepare, waits for trusted current-boot leases/share/credit, commits and enables admission. Runtime-less command paths reject apply. Prepared transactions resume after authenticated authorization; unsupported route/options fail explicitly.
- A real Linux subprocess Engine is SIGKILLed after TCP exchange and outstanding 512-byte reservation. Fresh boot obtains independently allocated authority, re-Prepares/Commits, resumes TCP, and retains at least 512 bytes unknown (in-flight real writes may add more). mTLS gRPC tests deliver the successor attestation and preserve old unknown debt across duplicate transport.
- `make phase5-check` requires Linux and an isolated <=16 MiB filesystem, runs root/Engine/contract/race checks and audits raw ledger snapshots. It fails on missing required tests/snapshots/hashes. Hub/backend authority is explicitly protocol-test-only. Final same-SHA paired results are still pending; no plan SUMMARY or completion is claimed yet.

### Earlier incremental pair

Commit `fef53edf5a383765cc5e0750a4b79a137c98e0ee`: local clean Docker recovery gate passed; Actions `42126935831` succeeded. Raw evidence is archived under `artifacts/phase5/recovery/fef53ed/{docker,actions}`. This pair predates the integrated boot/authorization 1.1.0 implementation and is not its acceptance evidence.

## Final integration refinements

- Restart tracer now uses the actual mTLS gRPC config/authorization path on both process lifetimes. Prepared activation retries on a bounded 100 ms timer while boot-local conservative time reaches NotBefore. Initial usage replay cannot end the configuration stream before final activation.
- A durable post-handoff control outbox carries revocations independently of new grants, including the all-delegations-revoked case. Revocation-only updates cannot reopen issuer/time authority; their receipt retains unknown liabilities. mTLS regression covers two handoffs, successor allocation and revocation with no new grant.
- `614b4f6` Docker full gate passed. Actions run `42130468946` failed the preflight on the untracked-file preflight before tests (file identities were not logged); it is not acceptance evidence. Gate diagnostics now list untracked build inputs, while unrelated root files outside the build-input directories do not count as source changes. The next pair must pass on the final source SHA.

- `6448c7d` Docker full gate passed. Actions `42131377838` identified the preflight cause: preceding Python contract tests generated untracked `tools/phase1/__pycache__/*.pyc`. Generated Python bytecode is now ignored; actual untracked source still blocks the gate.
- Final safety audit added runtime fencing checks: Commit rejects a prepared generation below the durable authorization floor, and new-flow allocation reads the floor in the same SQLite transaction as authority/slot allocation. A raised floor blocks new flows without discarding the original bounds of an existing draining session. Targeted Agent/store/Hub race tests pass; the final clean-SHA pair follows this change.
