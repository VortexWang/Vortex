# Phase 4 current-SHA evidence (2026-11-07)

## Native qualification update

The user-authorized Ubuntu 24.04 host `43.254.164.7` completed the formal
runtime-authority workload from clean commit
`edb1849c92b2cf81c2f6c2f9e91725f62c407090`. The host reports Linux amd64,
12 logical CPUs, 31.3 GiB RAM, native driver placement and qualification
interface `enp3s0f0`; the worktree was clean. The run used `GOMAXPROCS=1`, 16
TCP streams, 100 Mbps UDP, a 60-second semantic warmup and three 300-second
trials (`trial-1`, `trial-2`, `trial-3c`). The selected trials measured TCP
`635.085192`, `631.061724`, and `632.175350` Mbps; UDP was `99.998336`,
`99.998336`, and `99.997472` Mbps with zero loss. Source credit was conserved
and settled in every trial. The independent TLS comparison measured runtime
mixed TCP `635.085192` Mbps versus TLS mixed TCP `572.090168` Mbps, ratio
`1.110113803059267`, with zero UDP loss. The machine-generated qualification
gate returned `status=pass`, three trials, and 60-second warmup.

Summary hashes (raw event JSONL remains on the host) are:

- qualification gate: `038e20c64133a5fc9be6a341aab40aaf71f67ecb0e0785028d7564568d4da9b1`
- trial-1 host: `e6afe678f7ac2d8f3755bea5a6037f633b3297151a43f09acc442f72145fab33`
- native modes summary: `c60dfe04d85b0acea2cd4e56efc5251ead84ffd91df8a686a6aacceb681bd60c`
- native latency gate: `f6ff38409668bfdd5401a1b2b9c4e220c45dec1f5a646b2e410aa5efcbfeb330`

The same commit completed `BACKEND=native PHASE4_FORMAL=1 make
phase4-runtime-modes`; temporary UDP socket limits were restored to
`rmem_max=212992` and `wmem_max=212992`. The pool/mux controlled-RTT gate
passed 50 fresh samples per mode: pool additional median `0.991130 ms` and
mux `1.049556 ms`, both below 5 ms. Auto observed both pool and mux selection.
This closes the current Engine/tracer P4-Q5 gate; it does not include
Agent/Hub persistence latency.

The current Phase 4 branch was exercised from clean commit
`edb1849c92b2cf81c2f6c2f9e91725f62c407090`. The runs below are independent
outputs under `/tmp`; their summary hashes are recorded so the raw evidence can
be retrieved and checked without rewriting it into the planning tree.

## Q2 finite attack and recovery boundary

`ATTACK_DIR=/tmp/vortex-phase4-attack-current-20261107 tools/phase4/attack.sh`
passed all 16 configured-route forged-OPEN cases (eight TCP and eight UDP),
UDP link loss, and the attack gate with no reasons. The source SHA file hashes
to `85050f0461ad9704dd99ce2d3298dedf6a5fbf916bdf4def6705c71eed73969b2`.

`python3 tools/phase4/recovery.py --backend native` passed all eight finite
hot-reload/drain/restart cases: rejected unlisted/stale reload, failed
prewarm preservation, old TCP half-close, stale old OPEN rejection, downstream
generation switch, primary failure, active-middle failure without bypass, and
authorized spare restart. `recovery.json` hashes to
`1a5f9c17fab6d34ddb059e3ee029367b6a0555af1159180de255145112b5e21a` and its
checksum manifest hashes to
`83c6beb1e02618d7df88a925317dfb5147eaa3b4caa48e87329aeacb3d054f0d`.

This closes the enumerated current-configuration Q2 tracer matrix. It does not
claim universal future attack coverage, failover availability for an
unconfigured path, or durable activation and ledger recovery.

## Q3 snapshot and source-credit boundary

The native immutable snapshot run passed existing-session affinity, new-session
selection, startup-pinned routing, and old/new snapshot settlement with zero
sessions or held bytes. `snapshot-result.json` hashes to
`20c1d86b5dad2fa3e3b64f32b2362b4b9cca33dccb4efc228faa6c06818a7836`.

The native source-credit runs passed both protocols on the same commit:

- TCP: eight 16 KiB exchanges, exhaustion rejection for existing and new
  flows, exact 262,144-byte conservation, no held/unknown/session leak.
- UDP: 64 1 KiB datagrams plus four interleaved TCP exchanges, then existing
  and new UDP/TCP exhaustion rejection, exact 262,144-byte conservation, no
  held/unknown/session leak.

The TCP and UDP result hashes are respectively
`be9f462358ac85307fd7bcb9861f30cd96209cb021e8969b3177bc0f0f6533d7` and
`edd9fa46a4283d924397903858bcf16df12e76659c059a99b266ee9329c9e5bd`.

These are finite Engine and process-local adapter boundaries. They do not
establish a remote issuer, durable Agent/Hub receipt ledger, PostgreSQL balance
authority, or persistent recovery guarantee.

## Q4 current candidate scan

`python3 tools/phase4/d04_run.py --duration 5 --repeats 3 --rate-mbps 100`
completed all 21 one-factor trials on this commit. Every trial reported clean
worktree identity, exact source conservation and settlement, zero unknown upper
bound, and zero UDP loss. Dynamic targets observed the 8 MiB floor and the
configured 10/30/60-second ceilings of 125/375/750 MB.

The summary hash is
`f871725dcda3c96c40882b4d11bf046afd766a5d3d2471e51530ea369acf2d28`; the
checksum manifest hash is
`1cd046728fb685aff6ae014f98c2600d5a85c474da13cdae5701aaaec6cfb923`.
The result remains `measured-candidates-require-review` with `locked=false`.
The workload is five seconds per phase, uses the process-local issuer and does
not observe remote merge, durable occupancy, or idle-return proof. D-04 is
therefore still an acceptance debt.

## Q5 transport-mode regression

The formal-semantic Docker/OrbStack sweep completed explicit `pool`, `mux`, and
adaptive `auto` modes on this same commit. All three passed accounting,
forged-OPEN, bypass, binary-integrity, and middle-hop failure checks with zero
UDP loss. Fresh first-byte medians were 0.5150 ms (`pool`), 0.5754 ms (`mux`),
and 0.5789 ms (`auto`); observed TCP pool maxima were 4, 1, and 4. The mode
summary hash is
`1211328577dabc064a30ec6e7a06215f6c2b554fa5fe22079de9fde756ddb045` and its
checksum manifest hash is
`0a3228f15d23d86a5c836b718b3f3e1496bed462bd6d74d7cf037114514425d7`.

This is a Docker/OrbStack five-second diagnostic. It strengthens current-code
mode coverage but does not close native Linux controlled-RTT NFR-007 or the
formal performance gate.

## Native qualification access check (historical)

On the earlier 2026-11-07 access check the documented Ubuntu host
`43.254.164.7` was checked with
`BatchMode=yes`, a five-second connection timeout, and the existing dedicated
keys `vortex_deploy_ed25519` and `vortex_phase02_de1`. Both attempts reached the
host but returned `Permission denied (publickey,password)`. No password was
retrieved or used, and no native workload was started. That boundary was
subsequently resolved by the user-provided password-authorized session recorded
above; it remains historical context only.

The local reproducible checks for this evidence update all passed: 25 Phase 4
Python tests, Python compilation, shell syntax validation, `go test ./...`,
`go test -race ./...`, and `go vet ./...` from `engine`.

## Remaining Phase 4 boundary

Q2 and Q3 are complete within the explicitly bounded Engine/tracer scope. The
implementation is complete, while the following remain intentionally open:

- P4-Q1 formal native performance qualification: **passed** on the current
  commit; raw host artifacts and the selected trial reports remain on the host.
- P4-Q4 D-04 parameter lock: candidate observations are complete for this
  adapter, but no production default is approved.
- P4-Q5 native controlled RTT/NFR-007 qualification: **passed** for the
  configured Engine/tracer route; control-plane persistence latency remains a
  later integration concern.
- Persistent Agent/Hub authority and PostgreSQL business authority: owned by
  the Phase 5/6 handoff, not claimed by this Phase 4 tracer evidence.
