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

