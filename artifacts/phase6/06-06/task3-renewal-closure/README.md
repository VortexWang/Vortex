# 06-06 Task3 old-signer renewal closure — development evidence

Candidate source, not final Phase6 acceptance or same-SHA GitHub Actions pairing.
Go1.27.3; sqlc1.31.1; protoc36.2 / protoc-gen-go1.36.12 / protoc-gen-go-grpc1.6.2.

- `sqlite-contracts-race.log`: canonical bounded pages; injected SQLite write rollback; persistent tombstones, reopen and exact replay; late installs and new issues denied, prior issue bytes preserved.
- `backend-focused-race.log` and `backend-transport-race.log`: PG audited barrier/receipt rollback, two producers, complete65-delegation snapshot, independent kind, real Node CAS-only release, TLS1.3 role roots, lost FULL closure/PG ACK replay and legacy funded/usage-only capability isolation.
- `backend-all-race.log`: full native backend race suite against isolated closure database.
- `agent-hub-contracts-race.log`: full native Agent/Store/Hub/contracts/Node CLI race suite.
- `linux-race-build.log`: Linux arm64 Docker service/control/worker/timeauthority race, explicit migration before parallel test processes, amd64/arm64 backend builds, focused Store/Hub/contracts/Node CLI race. Cross-compiled amd64 is not native execution/performance evidence.
- `generators.json`: two independent clean sqlc/protobuf generations, all20 files match checkout.

Retained failures: `backend-transport-race-initial-failed.log` is a test injection setup error (the observation-only negative case consumed the lost-receipt flag before the real funded client); flags now arm after negative cases. `agent-hub-contracts-race-path-failed.log` includes passing package results but invalid root `cmd/vortex-hub` path; Hub CLI lives in backend module, whose full suite passed. `linux-race-build-migration-race-failed.log` records parallel first migration on a fresh DB colliding on goose_db_version; corrected entry explicitly migrates once before tests. Initial CLI compile also referenced a nonexistent Adapter.BackendID; corrected to the existing pinned backend-id flag before passing tests. An initial host test command had an incorrect log directory and did not start tests; rerun from backend directory.

The SQLite write-fault trigger proves atomic rollback, not a physical power-cut/fsync experiment. Production closes old delegation issuance, not extant child leases, parent reservations or unknown financial debt. Sustained funding/parent-child closure and explicit boot recovery remain incomplete. CR-008 is not yet proposed or approved. Phase6 stays8/11; final paired Actions belongs to06-11.
