# P4-Q5 transport mode regression evidence (2026-11-06)

`sh tools/phase4/modes.sh` completed on clean commit
`f0bb54aaa4bc4838e4c811c1ffec8b3a48ed7ecb` with Docker/OrbStack, explicit
runtime authority, 5-second phases, four TCP streams and 100 Mbps UDP. Raw
output is retained at `/tmp/vortex-phase4-modes-fdaea8f`; `summary.json` and
`checksums.sha256` cover all three modes.

| Mode | Fresh first-byte median | Segment RTT sum | Maximum TCP pool | Link reuse |
| --- | ---: | ---: | ---: | --- |
| pool | 0.5670 ms | 0.0376 ms | 4 | exclusive pool leases observed |
| mux | 0.5427 ms | 0.0230 ms | 1 | shared mux link observed |
| auto | 0.5320 ms | 0.0368 ms | 4 | both mux and pool observed |

All modes passed finite binary integrity, forged OPEN, direct bypass and
middle-hop failure checks. The auto run observed both transport choices for
source and all three relay roles; mux kept one TCP pool link and pool exercised
multiple exclusive leases. UDP remained on the shared UDP mux path.

This is a final-code regression and transport-mode behavior record. It does
not satisfy the native Linux Q5/NFR-007 gate: the host is an OrbStack Docker
VM, the samples are userspace connect-to-first-byte measurements, and the
formal gate still requires clean native Linux mode pairs and controlled RTT
evidence. No Agent/Hub persistence latency is included.

