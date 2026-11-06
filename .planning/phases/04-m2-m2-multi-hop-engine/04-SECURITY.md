---
phase: "04"
slug: "m2-m2-multi-hop-engine"
status: blocked
threats_open: 1
asvs_level: 1
created: "2026-11-05"
---

# Phase 04 — Security

## Current ownership correction — 2026-11-06

Per owner-approved [04-CLOSURE.md](04-CLOSURE.md), the production durability
part of T-04-02 belongs to X5/X6 and cannot block Phase 4 advancement. This does
not waive the Engine-side funded-adapter conservation/admission checks in Q2/Q3.
The historical blocked verdict below is not the current stage-closure rule.
Security qualification remains incomplete pending the explicit Phase 4 matrix;
no production security acceptance or risk waiver is implied.

## Historical review

> In-window retroactive STRIDE review of the Phase 4 tracer implementation.
> This is not a production security sign-off. No user risk acceptance is recorded.

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Local source client to vortex-relay | Local TCP/UDP listener accepts application traffic | Payload, flow metadata |
| Relay to relay | TLS 1.3 mTLS VLink connection with HELLO identity and route checks | OPEN, DATA, DGRAM, control frames |
| Relay to configured target | Execution point dials a locally configured target | Authorized stream payload |
| Source runtime to accounting Gate | Optional in-process credit authority; not durable or authoritative | Reservations, outcomes, grant identity |

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-04-01 | Spoofing | VLink peer identity | high | mitigate | TLS 1.3 mTLS, canonical node URI SAN, HELLO signature bound to exporter, peer policy | closed |
| T-04-02 | Elevation of privilege / repudiation | Authoritative source accounting | high | mitigate | Default source execution now fails closed without an explicit test mode or Gate; production grant authority, durable settlement, and complete source conservation remain unimplemented | open |
| T-04-03 | Tampering | OPEN path authorization | high | mitigate | Check local rule revision, path generation, hop index, visited nodes, peer membership, network and local target | closed for finite tracer cases |
| T-04-04 | Denial of service | VLink parser, windows and transport pools | medium | mitigate | Bound frame sizes, windows, queues, pool links and stream violations | closed for implemented finite limits |
| T-04-11 | Elevation of privilege | Source startup and qualification mode | high | mitigate | Reject missing source authorization by default; qualification-only must be explicit and visible in test metadata; missing Gate never forwards payload | closed for default runtime boundary |

### Evidence

- `engine/runtime/config/config.go:195-249` constrains TLS to 1.3, requires a
  trusted certificate and checks the local certificate node identity.
- `engine/vlink/identity.go:46-72` requires TLS 1.3 and invokes peer
  certificate policy; `engine/vlink/hello.go` signs the HELLO transcript with
  TLS exporter input.
- `engine/vlink/open.go:76-156` rejects stale rule/path revisions, invalid
  hops, unlisted peers, invalid networks, repeated/local visited nodes and
  missing local authorization fields.
- `engine/runtime/config/config.go` requires an explicit qualification-only
  mode or source authorization configuration and rejects qualification mode
  without its evidence file.
- `engine/runtime/relay/policy.go` no longer asserts runtime, segment,
  rate-share, connection-share or billing authorization by default.
- `engine/runtime/relay/bridge.go` rejects a nil Gate; regression tests prove
  that the missing-Gate path does not write payload to the target.
- `engine/cmd/vortex-relay/main.go` keeps qualification-only behavior explicit;
  Phase 4 generated test fixtures opt in and record the mode in metadata.
- Same-clean-SHA local Docker and Actions evidence is recorded in
  `04-07-EVIDENCE.md`; Actions run 41887199379 completed successfully.
- The in-process authorization adapter is not an authoritative or durable
  Agent/Hub ledger. This residual keeps T-04-02 open and prevents production
  accounting sign-off.

## Accepted Risks Log

No accepted risks.

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-11-05 | 5 | 4 | 1 | Codex in-window review and Plan 04-07 evidence |

## Sign-Off

- [x] Threat dispositions recorded
- [x] Unaccepted open threat remains explicit
- [ ] `threats_open: 0` confirmed
- [ ] `status: verified` set in frontmatter

**Historical approval:** blocked for production accounting. Superseded as a Phase 4 advancement rule by the 2026-11-06 ownership correction above.
