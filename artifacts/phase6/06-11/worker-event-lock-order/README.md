# Operation-event foreign-key lock order

Historical source30653e9b Actions42554576162 amd64 failed `TestTracerRealProcesses`; raw backend log reports `port operation worker: ERROR: deadlock detected (SQLSTATE 40P01)`. arm64 completed including real Cloudflare, but the source has no passing three-host pair. a2097430 Docker/Actions were interrupted/canceled when this independent defect was confirmed; not accepted.

Operation event insertion references tenants. A task/operation owner can therefore wait for tenant FOR UPDATE even for query/abort/release/projection. All worker event writes now follow tenant -> task/operation; the initial task-only lease is committed before waiting, then exact token/state/deadline are rechecked. Release reconstructs the immutable old artifact through observation only, so a suspended account cannot prevent stop; it grants no new activation. Projection follows tenant -> group -> allocation -> task.

Control expiry uses the existing bounded query in a rolled-back discovery transaction, then rechecks and atomically commits task unknown plus operation event under tenant -> task. A process loss leaves an expired attempt discoverable; event failure cannot strand task unknown with operation pending. No schema/SQL/frozen artifact changed.

- `red-a209.log`: old source plus the new behavioral test; all six original paths fail55P03, repeated3 times.
- `green-complete.log`: initial six paths pass repeated3 race runs after lock fixes.
- `process-green.log`: eight paths including lease supersede, expiry-event fault rollback/retry, previous rule authority lock test, real backend/Agent/Hub processes, actual Agent socket unknown/retry, port aggregate/release proofs and rollback; Linux race repeated3 passes49.941s.
- `native-green.log`: final equivalent native focused race outcome.
- `evidence-gate.log`: runtime gate requires both new tests; removing a lock proof fails despite refreshed hashes.3 groups pass.
- `green.log`: development command used default Compose ports already owned elsewhere and failed before tests. `green-corrected-ports.log` uses this attempt's allocated ports and passes. Only this project's stopped services were recreated; no other project stopped.

These focused development results are not a substitute for new clean-source full Docker/native dual Actions acceptance. Existing60s reconciliation and5s regional failover bounds stay unchanged. Historical failed raw folders remain unchanged.
