# Phase 2 TCP Benchmark

`make phase2-benchmark` compiles the relay benchmark for the Docker host's native
Linux architecture, runs it in a disposable Linux container, and writes a
timestamped report under `artifacts/phase2/benchmark/`.

The benchmark uses persistent real TCP connections and the bare `relay.TCP`
path. On supported Linux architectures that path selects `splice`. Client,
relay, and sink share the host-local loopback network, so the result measures
relay-path throughput in that topology rather than an external NIC link. It
reports Go's raw benchmark output, measured throughput, and the difference
from the 700 Mbps development target.

OrbStack and GitHub-hosted runner results are `environment-limited`. They help
compare code changes but do not establish qualified performance acceptance.
The harness records `unmeasured` if the benchmark output has no throughput
value or if execution fails.
