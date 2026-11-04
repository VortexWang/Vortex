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

The mixed check starts source → relay-1 → relay-2 → relay-3 → destination,
plus a fixture echo target. It verifies a TCP half-close payload and UDP payload,
then runs four TCP flows for five seconds as a contention baseline, followed by
four TCP flows with 1200-byte UDP packets offered at 100 Mbps for five seconds.
TCP traffic runs in a separate process from UDP generation. Payloads are checked
and TCP responses must drain completely. The fixture stops and awaits its
processes/containers before reading accounting evidence.

The fixture uses TCP ports 18080/18083 and UDP port 18082 on the host. Port 18083
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
