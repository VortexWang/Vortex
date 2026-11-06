# D-04 dynamic target scan evidence (2026-11-06)

The D-04 runner now passes each target-seconds factor through
`CreditConfig.TargetSeconds` and records the bounded credit timeline. The
complete 21-trial scan used clean commit
`e862ffef040e94f7099f3570a238245561e774fd`, with output at
`/tmp/vortex-phase4-d04-e862ffe-dynamic-5x3-rerun` and checksums for every
trial.

Every trial had `dirty=false`, zero UDP loss, actual offered UDP between
99.913 and 99.959 Mbps, exact source conservation, zero unknown/pending/held
bytes at close, and a nonzero dynamic-target timeline. The observed target
range was:

| Factor | Dynamic target range |
| --- | ---: |
| 10 seconds | 8 MiB floor to 125,000,000-byte ceiling |
| 30 seconds baseline | 8 MiB floor to 375,000,000-byte ceiling |
| 60 seconds | 8 MiB floor to 750,000,000-byte ceiling |

All three block factors (64/256/1024 KiB) and waterlines (20/30/50%) were
executed three times with 16 TCP streams, 1,200-byte UDP and 50 short-flow
samples. The measured mixed TCP range was 2,881.44–2,946.39 Mbps and peak
held bytes stayed bounded at 130,515,488–135,827,840 bytes. Results remain
`measured-candidates-require-review`, `locked=false`.

The prior dynamic run is retained at
`/tmp/vortex-phase4-d04-e862ffe-dynamic-5x3`; it failed closed after 13
successful trials when one short-flow probe saw `latency echo truncated`. This
was a host transient, not a conservation failure, and demonstrates the scan's
fail-closed behavior.

The scan still measures a synchronous process-local issuer. It does not
provide remote in-flight merge, durable funding occupancy, idle return proof,
or formal native-host performance qualification. No D-04 production parameter
is locked.

