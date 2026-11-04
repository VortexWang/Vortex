# Authorized route activation and recovery — 2026-11-04

Plan 04-06 source SHA `d0c1cb22c88cb751c86057e9a500f2595fc7533a`.
This extends the real tracer binary; earlier library-only generation tests
were not counted as running-path switch evidence.

## Implementation

The optional `routes` map pins local authorized segments at process start.
Each entry specifies outgoing address, authenticated next-hop identity,
authorized previous identity and hop index. SIGHUP rereads the config, accepts
only a configured `active_route` with strictly larger `path_generation`, and
rejects candidate/identity/listener/rule/target edits. The selected outgoing
segment is authenticated and prewarmed before local activation. Failed prewarm
leaves the old generation active. A trusted but unexpected next-hop certificate
identity is rejected. The adversarial bypass probe explicitly bypasses that
client-side pin so it can still test the receiving policy boundary.

Activation swaps the new admission generation atomically. New old-generation
OPENs fail; existing TCP transport leases remain until the worker releases
them. Old UDP generation contexts are cancelled, and later datagrams require
new OPEN/OPEN_OK. Acquisition checks again after dialing to reject a concurrent
activation. Reference counts retain at most sixteen draining generations;
additional reload is rejected until drain makes space. Auto history follows
the active generation rather than a retired transport.

This is locally controlled tracer activation, with downstream-first/source-last
orchestration. It is not a distributed Prepare/Commit transaction, automatic
health controller or crash-durable generation authority. Startup config is
trusted local input; there is no network management endpoint. A process restart
loads its current file and does not restore former TCP streams or memory history.

## Actual process regression

The fixture starts source, three relay positions, destination and a spare
middle relay with its own certificate/node ID. It pins both three-relay routes:
`source → relay-1 → relay-2 → relay-3 → destination`, and
`source → relay-1 → relay-2b → relay-3 → destination`.
No relay position is skipped. Test traffic and signals operate actual sockets
and binary processes, not mock forwarding or path-coordinator state alone.

| Case | Verified behavior |
|---|---|
| Unlisted route and stale reload | Both rejected; primary traffic still forwards |
| Unavailable authorized spare | Prewarm fails; active generation stays primary |
| Old TCP graceful drain | Target response and half-close EOF still work after activation |
| Fresh old-generation OPEN | Actual TLS/HELLO probe receives stale error code 7 |
| Authorized generation switch | Destination-to-source activation reaches generation 2; fresh TCP/UDP work through spare |
| Primary relay stop | Old TCP terminates; new alternate TCP/UDP remain usable; old source generation drains |
| Active spare stop | Established TCP terminates, fresh TCP cannot bypass/fall back, and UDP cannot forward |
| Spare restart | Fresh TCP/UDP recover at generation 2; the datagram sent during failure is not replayed |

The UDP generation-1 close and generation-2 OPEN are also required in source
logs. A normal old TCP stream drains gracefully; a separate old stream remains
to test primary-path failure. The restart check sends a unique fault datagram,
expects silence during outage, then requires the first returned packet after
restart to equal the new datagram. Old established TCP is never replayed.

## Same clean SHA verification

```sh
DOCKER_CONTEXT=orbstack GOMAXPROCS=1 python3 tools/phase4/recovery.py \
  --backend docker --out artifacts/phase4/recovery/docker-d0c1cb2

GOMAXPROCS=1 python3 tools/phase4/recovery.py \
  --backend native --out artifacts/phase4/recovery/native-d0c1cb2
```

- Local OrbStack Linux arm64 Docker: all eight cases pass, `dirty=false`.
- Authorized Ubuntu 24.04.5 amd64 KVM host: all eight pass, `dirty=false`;
  full Engine race also passes. The host has four vCPUs/approximately 8 GiB;
  all roles share it. This fixture establishes behavior, not throughput.
- Local `go test -race ./...` and `make phase4-m2-check` pass. New rejection
  tests cover pinned candidate/target/identity mutation, stale admission and
  generation lease removal. Existing TCP/UDP unknown-target-short-write tests
  remain passing and preserve full unknown upper bounds after session closure.
- Same-SHA [Actions run 41836183498](https://github.com/VortexWang/Vortex/actions/runs/41836183498)
  succeeded, including full shared checks, scale/mode regressions, the new
  Docker recovery target and evidence upload.

Local reports/logs: `artifacts/phase4/recovery/docker-d0c1cb2` and
`artifacts/phase4/recovery/native-d0c1cb2`. The native full evidence remains at
`/root/Vortex/artifacts/phase4/recovery/native-d0c1cb2`; copied files exclude
private configs/certificates and accounting JSONL. Both reports include startup
config SHA-256 hashes and clean commit identity. All eight Docker and nine
native evidence checksums pass. The extra native log is the echo target.

## CI fixture repair

Runs 41833628991 and 41834048045 failed in the new fixture: `compose up`
could recreate the spare, making an accumulated-log ready count invalid;
the spare accounting evidence was also created by container root with mode
0600, preventing runner artifact upload. These runs are failures, not passes.
The fixture now starts the existing stopped container with `compose start`,
precreates its accounting file as the host user (like the original roles),
and prints role logs when a regression fails. Both local Docker and native
reruns above use this final repair SHA.

## Boundaries and next work

These finite tests now cover real authorized switching, graceful drain,
failure-stop behavior and fresh-session recovery. They do not establish
exhaustive fault combinations, durable Agent/Hub recovery, authoritative quota
release, production orchestration or a qualified long-duration topology.
The memory adapter is not claimed to survive its own process loss. Unknown
liability conservation remains covered at its existing in-process boundary.

Phase 4 remains executing. Next are D-04 parameter review and explicit mapping
of remaining NFR host/link/duration criteria, plus any uncovered fault cases
required by the final Phase 4 review. No 04-06 SUMMARY or full NFR completion
is created by this evidence update.
