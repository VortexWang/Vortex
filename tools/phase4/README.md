# Phase 4 runtime diagnostics

Run from the repository root with Go 1.27.3, Python 3 and OpenSSL installed:

```sh
make phase4-m2-check
make phase4-runtime-mixed
BACKEND=docker make phase4-runtime-mixed
```

On a Mac configured for OrbStack, an explicit Docker context can be selected:

```sh
orbctl start
DOCKER_CONTEXT=orbstack BACKEND=docker make phase4-runtime-mixed
```

Use the Docker backend for the complete benchmark on macOS:

```sh
DOCKER_CONTEXT=orbstack BACKEND=docker make phase4-m2-benchmark
```

The native backend remains useful for host diagnostics, but Darwin does not
expose the Linux UDP receive-buffer counters used to classify every loss in the
strict accounting gate. A native Darwin run with unexplained socket loss is
therefore expected to fail closed rather than being treated as qualification.

The mixed check starts source → relay-1 → relay-2 → relay-3 → destination,
plus a fixture echo target. It verifies a TCP half-close payload and UDP payload,
then runs four TCP flows for five seconds as a contention baseline, followed by
four TCP flows with 1200-byte UDP packets offered at 100 Mbps for five seconds.
TCP traffic runs in a separate process from UDP generation. Payloads are checked
and TCP responses must drain completely. The fixture stops and awaits its
processes/containers before reading accounting evidence.

The fixture uses TCP ports 18080/18083 and UDP port 18082 on the host by default. Set
`QUALIFICATION_TCP_HOST_PORT` or `QUALIFICATION_UDP_HOST_PORT` to avoid host port
conflicts; container-side service ports remain unchanged. Port 18083
is the echo target's test-only counter endpoint. The in-process accounting
adapter is bounded to one million events and is not a durable authority ledger.
Increasing `MIXED_DURATION_SECONDS` can exhaust that bound; an exhausted or
unresolved accounting run must fail.

Long-duration qualification can set `VORTEX_QUALIFICATION_EVENT_LIMIT` for the
fixture only (maximum 10,000,000 events per execution process). The default
remains 1,000,000, and the selected value is included in host/semantic evidence.
The override is ignored by runtime configs without an evidence file.
Events are retained in bounded 4,096-entry chunks to avoid large contiguous
slice copies stalling forwarding. Relay containers have a two-minute stop
grace to finish JSONL output. The fixture rejects nonzero relay exit codes,
and the relay reports evidence open/write/close errors as failures.

Evidence is written below `artifacts/phase4/qualification/{native,docker}`:

`make phase4-runtime-modes BACKEND=docker` exercises explicit `pool`, `mux`,
and adaptive `auto` configs on the real five-process route, with per-mode latency raw samples,
link-reuse evidence, finite fault probes, TLS comparison and byte reconciliation.
The native backend uses the same command without `BACKEND=docker`. Reports go
to `artifacts/phase4/modes/{backend}`. The tracer's optional `transport_mode`
selects a real authenticated outgoing link pool; omitted mode retains its
legacy connection strategy. `pool_min_links` defaults to 1, `pool_max_links`
to 4, with a maximum of 16 links per TCP or UDP peer pool. TCP `pool` leases
are exclusive for a stream's lifetime; `mux` shares links, and UDP always uses
a separate shared mux pool. Initial TCP links are prewarmed with bounded retry
backoff. Healthy idle links are reused; unhealthy ones are replaced on demand.
In `auto`, TCP begins in mux without samples, then uses the bounded ten-minute
first-target-byte median policy: under ten seconds selects pool, otherwise mux.
Full adaptive scaling remains outside this tracer. These config fields extend the tracer config,
not the separate Agent runtime configuration contract.

The mode summary keeps fresh client-connect-to-first-byte separate from the
userspace segment echo RTT sum. Their subtraction is diagnostic; this alone
does not qualify NFR-007 or prove that OPEN confirmation adds no network RTT.

Evidence files:

- `host.json`: clean/dirty SHA, host facts, exact generated-config hash and
  semantic manifest hash. The semantic hash excludes deployment addresses and
  ephemeral certificate paths; it must agree across backends. Exact hashes
  normally differ and must not be described as equal.
- `mixed-workload.json`: byte counts, first-byte samples, throughput, TCP
  retention, UDP sent/received/loss and target counters.
- `events-destination.jsonl`: actual committed execution-point accounting.
  Successful execution bytes and final client delivery bytes are distinct:
  a downstream drop does not undo an already committed write.
- `drop-summary.json`: bounded application window/queue drops and Linux UDP
  receive-buffer counters. Compose also samples each fixture UDP network
  namespace. Host/namespace counters are diagnostic, not per-flow proof.
- `report.json`: accounting and NFR/D-04 status. Unknown outcomes, unexplained
  losses, incomplete topology or mismatched accounting stop the check.
- `checksums.sha256`: hashes of recorded JSON, accounting and role logs.

`runtime_correctness=pass` means this short fixture run reconciled correctly.
Every performance NFR remains `not-qualified`: the TCP-only run is a contention
baseline, not an independent minimum TLS implementation. Dedicated host/path
eligibility, longer performance tests, independent TLS and latency baselines,
fault coverage and D-04 review are still required.


## Independent TLS, latency and runtime faults

After `make phase4-runtime-mixed`, run `make phase4-runtime-comparison` with the
same BACKEND and clean application SHA. Compose uses the same target, container
network and role configuration as the runtime. Native uses the same loopback
endpoints. The separate `tls-baseline` binary forwards with Go TLS 1.3 and
`io.Copy`; it does not execute VLink, mux or accounting. Each baseline TCP flow
has four TLS segments. Both implementations record the negotiated TLS cipher.

The comparison saves four-flow TLS throughput, 50 client-connect-to-first-byte
samples per implementation (five warmups excluded), and 50 established-data RTT
samples per segment. The RTT probes use test-only ALPN on the independent
baseline, including the same four network segments and destination-target leg;
they include userspace overhead and are not kernel TCP RTT measurements.

Runtime probes reject six forged OPEN variants with stable codes/correlation
IDs, reject direct source-to-relay-2/destination bypass, terminate an authorized
UDP stream after link loss, and stop relay-2 while established TCP/UDP flows are
active. Existing TCP must terminate; UDP and new TCP must not bypass the failed
hop. The fixture has no alternate authorized path, so it does not establish
failover availability. Raw runtime logs and JSON results remain under
`artifacts/phase4/comparison/{native,docker}` with checksums.

Runtime receive candidates now use 1024 UDP frame slots with a separate 1 MiB
byte cap (TCP remains 128 frame slots); queued byte capacity returns on dequeue.
This prevents a tiny-packet burst from consuming the full TCP frame-size budget.
It remains a bounded tuning candidate, not D-04 approval. Optional native
`CPU_PROFILE=1` writes per-role CPU profiles; profiling runs are diagnostic and
must not be compared as unprofiled performance evidence.

The comparison does not qualify NFR-007: actual pool/mux modes are not separately
measured. Performance ratios and latency differences are reported even when
thresholds fail. `flow-accounting.json` contains opaque link/stream/network keys;
TCP echo totals must match per flow, and aggregate execution totals still match
target counters. The in-process adapter remains non-durable.

The next comparison batch uses explicit `GOMAXPROCS=1` for all role processes
in both implementations (and both backends). This is a shared four-vCPU fixture
candidate, not a change to the product's Go scheduler default or a D-04 lock.
Use `GOMAXPROCS=1 make phase4-runtime-mixed` followed by
`GOMAXPROCS=1 make phase4-runtime-comparison`; add `BACKEND=docker` to each for
Compose. Environment choices are recorded and included in the semantic manifest.
The source UDP candidate queue is now 256 datagrams with a simultaneous 256 KiB
byte bound, compared with the previous 64-packet-only bound.

Compose now runs smoke, mixed throughput, latency and binary/fault traffic in a
separate Linux client container on the fixture bridge. Both runtime and minimal
TLS measurements use that client and the same endpoints. This avoids the
Darwin-to-VM UDP port-forwarding path; published ports remain available for
manual diagnostics. Native continues to use its host driver. Driver OS/CPU facts
and placement are recorded; the two hosts are not asserted hardware-identical.
A 64 MiB deterministic binary transfer is SHA-256 checked before middle-hop
failure injection. Startup readiness requires all three outgoing authenticated
relay links, rather than merely running processes or fixed sleeps.

`BACKEND=docker make phase4-runtime-recovery` exercises actual relay processes
with an independently identified spare middle relay. Native Linux uses
`BACKEND=native`. The opt-in tracer `routes` map pins local authorized segments
at startup; each entry contains `peer`, `peer_node_id`, `next_hop_node_id` and
`hop_index`. `active_route` selects an entry. Atomically replace the config and
send SIGHUP to activate a strictly larger `path_generation`. Other fields and
the candidate map cannot be changed by reload. Activate downstream first,
source last; this tracer does not implement a distributed activation transaction.
A failed authenticated prewarm preserves the current route. Old TCP leases
remain until drain; old UDP sessions terminate and new data requires a new OPEN.
At most 16 draining generations are retained; additional reload is rejected.
The fixture checks rejected unlisted/stale reloads and old OPENs, unavailable
candidate rollback, old TCP half-close, UDP generation restart, both primary
and active-middle failure, no unauthorized fallback and new TCP/UDP recovery
after process restart. Logs, startup config hashes, results and checksums are
saved under `artifacts/phase4/recovery/{backend}`. This is finite in-memory
tracer evidence, not durable Agent/Hub transaction or ledger recovery. Active
route selection is locally controlled, not an automatic health controller.

## Finite static-snapshot source credit

`BACKEND=docker make phase4-runtime-credit` (or `BACKEND=native` on Linux)
runs TCP and mixed UDP/TCP cases on the real three-relay route with a finite source authority. Eight
16-KiB echo exchanges consume a 256-KiB bidirectional budget through four
64-KiB local grant allocations. Further writes on the existing stream and a
new stream must deliver no extra target bytes. Shutdown evidence must show
exact consumption, no live sessions/held tails, and conservation. Native and
Compose share this workload; results, semantic/config hashes, logs and checksums
are under `artifacts/phase4/credit/{backend}` and `{backend}-udp`. The latter
interleaves 64 1-KiB UDP echoes with four 16-KiB TCP echoes against one 256-KiB
budget, then rejects old/new UDP and new TCP traffic after exhaustion. The probe runs on the host (unlike
the throughput driver's Linux container), and measures correctness only.

Optional tracer configuration on a source:

```json
"execution_point": true,
"transport_mode": "mux",
"source_credit": {
  "snapshot_id": "rule/execution/path-1/policy-1",
  "budget_bytes": 33554432,
  "initial_block_bytes": 262144,
  "max_block_bytes": 4194304,
  "grant_target_bytes": 8388608,
  "renew_percent": 30
}
```

`evidence_file` is required. Credit evidence is saved beside it with suffix
`.credit.json`. This mode explicitly rejects hot route sets,
non-source roles and max blocks smaller than the bridge's 64-KiB read buffer.
Each authority owns one immutable startup snapshot and a raw-byte budget;
bridge directions are connection-relative. Sessions share that one budget,
allocate growing per-direction blocks and return unused tails on close.
Outstanding/unknown reservations cannot be reclaimed. A depleted direction may
return the opposite direction's unused tail to the same authority. Unknown
bytes remain occupied, and duplicate Batch resolution is rejected.

The local allocator moves bytes from unissued to available at the configured
waterline, bounded by remaining budget. `BeginGrant` / `ApplyGrant` also support
trusted in-process request/result injection: request/snapshot/amount binding,
grant-ID uniqueness, idempotent retries, pending/unknown issuance occupation,
and late results. Unknown issuance releases only on a matching acceptance or
an explicitly injected proof of non-issuance, never elapsed time. Request/grant
records and live credit sessions each have a 4,096-entry bound; reused IDs are
not evicted. Local allocator IDs have a reserved namespace. Unused block tails
can be returned from other sessions of this same authority under scarcity;
in-flight writes and unknown bytes cannot. There is no remote issuer,
async network client, 200-ms network deadline, rolling-rate target, idle reclaim or
coefficient conversion. It is non-durable: restarting starts a new test budget,
so it must never authorize production traffic or claim recovery guarantees.
Both UDP directions reserve before write; explicit VLink queue refusal returns
the reservation, ambiguous network writes retain it as unknown. The UDP worker
is joined before its credit session closes. It does not implement full
billing-snapshot switching or lock D-04. Existing
qualification fixtures set `qualification_only` explicitly and use the bounded
in-process evidence Gate; ordinary source configs require a positive funded
`source_credit` or active `source_credit_book` snapshot.
