# D-04 candidate scan evidence (2026-11-06)

The executable one-factor scan completed on clean commit
`ae858f52a08d20595ebe77f56628cfc0060616ab`. Output is retained at
`/tmp/vortex-phase4-d04-ae858f5-5x3` with 21 per-trial JSON reports and a
top-level `checksums.sha256`. Every trial recorded `dirty=false`, conserved
and settled source credit, zero unknown bytes, zero UDP loss, and actual UDP
offered load between 99.698 and 99.957 Mbps. The summary status is
`measured-candidates-require-review` and `locked=false`.

The scan used the bounded process-local issuer, 16 TCP streams, 1,200-byte UDP
at a requested 100 Mbps, 50 short-flow samples, and 5 seconds per baseline and
mixed phase. Issuer timing is the local allocator only; it is not a network Hub
or durable transaction measurement.

| One-factor case | Mixed TCP Mbps range | Short-flow median ms | Peak held bytes |
| --- | ---: | ---: | ---: |
| baseline 256 KiB / 30 s / 30% | 3100.72–3113.74 | 0.4714–0.4768 | 125,468,760–135,255,432 |
| block 64 KiB | 2735.23–3085.25 | 0.4468–0.4695 | 129,294,640–134,903,436 |
| block 1024 KiB | 2978.57–3109.10 | 0.4785–0.4839 | 130,182,924–132,707,560 |
| target 10 s | 3096.02–3117.98 | 0.4624–0.4756 | 129,008,040–133,173,768 |
| target 60 s | 3112.07–3135.58 | 0.4595–0.4761 | 130,478,152–134,487,528 |
| waterline 20% | 2936.64–3091.68 | 0.4594–0.4740 | 129,093,188–133,851,864 |
| waterline 50% | 2906.69–2996.35 | 0.4571–0.4732 | 131,084,400–132,802,144 |

The observed spread is not a production parameter decision. The scanner does
not observe a remote issuer's in-flight merge rate, durable funding occupancy,
or idle return proof; those are outside this adapter. No candidate is locked.

## Long-load boundary

A separate 15-second attempt on the same host and SHA failed closed during the
first baseline case: mixed UDP loss reached 41.35% and several TCP streams
reset, with the driver reporting only 58.64 Mbps received UDP. The failure is
retained at `/tmp/vortex-phase4-d04-ae858f5-15x3` and is evidence that this
Darwin host cannot supply the sustained D-04 or formal performance gate.

The scan therefore closes the missing short candidate comparison for the
process-local adapter, but it does not lock D-04, qualify NFR-003/NFR-005, or
substitute for Agent/Hub persistence and network issuer behavior.

