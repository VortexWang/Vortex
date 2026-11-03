# Phase 4 Restart Validation Strategy

| ID | Behavior | Evidence | Command |
| --- | --- | --- | --- |
| R04-01 | One binary validates source/relay/destination configs and certificates | unit tests + invalid-config fixtures | `go test ./runtime/...` |
| R04-02 | Five-process topology starts and health-checks on one authorized path | process logs, topology digest, clean shutdown | `make phase4-runtime-smoke` |
| R04-03 | TCP payload crosses exactly three relays with ordering/checksum and half-close | source/destination transcript and visited-hop audit | `make phase4-runtime-smoke` |
| R04-04 | Forged OPEN, stale generation and bypass route are rejected | stable error codes and audit JSONL | `go test -race ./engine/... ./runtime/...` |
| R04-05 | Accounting commits successful writes once and retains unknown upper bounds | adapter event ledger and invariant checker | `go test -race ./runtime/accounting/...` |
| R04-06 | UDP requires OPEN/OPEN_OK, uses bounded queues and reports classified drops | sequence/checksum/loss and scheduler counters | `make phase4-runtime-mixed` |
| R04-07 | Compose and native launchers use equivalent config/workload | same config hash and normalized result schema | `make phase4-runtime-smoke BACKEND=docker` and `BACKEND=native` |
| R04-08 | Performance qualification refuses missing runtime or accounting evidence | non-zero exit and blocked verdict | `make phase4-m2-benchmark` |
| R04-09 | Same SHA runs on local native host and GitHub Actions | host metadata and uploaded raw artifacts | workflow `phase4.yml` |
| R04-10 | NFR-003/NFR-005/D-04 review uses three-relay mixed load, not loopback smoke | signed review record | qualification runbook |

The first plan must implement R04-01 through R04-05 as a vertical tracer slice.
R04-06 through R04-10 depend on a passing topology health gate.
