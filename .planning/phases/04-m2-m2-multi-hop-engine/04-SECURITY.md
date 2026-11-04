---
phase: "04"
slug: "m2-m2-multi-hop-engine"
status: blocked
threats_open: 1
asvs_level: 1
created: "2026-11-05"
---

# Phase 04 — Security

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
| T-04-02 | Elevation of privilege / repudiation | Source runtime authorization and accounting | high | mitigate | Require funded source authorization and reject traffic when the Gate is absent; do not treat tracer authorization flags as production grants | open |
| T-04-03 | Tampering | OPEN path authorization | high | mitigate | Check local rule revision, path generation, hop index, visited nodes, peer membership, network and local target | closed for finite tracer cases |
| T-04-04 | Denial of service | VLink parser, windows and transport pools | medium | mitigate | Bound frame sizes, windows, queues, pool links and stream violations | closed for implemented finite limits |

### Evidence

- `engine/runtime/config/config.go:195-249` constrains TLS to 1.3, requires a
  trusted certificate and checks the local certificate node identity.
- `engine/vlink/identity.go:46-72` requires TLS 1.3 and invokes peer
  certificate policy; `engine/vlink/hello.go` signs the HELLO transcript with
  TLS exporter input.
- `engine/vlink/open.go:76-156` rejects stale rule/path revisions, invalid
  hops, unlisted peers, invalid networks, repeated/local visited nodes and
  missing local authorization fields.
- `engine/runtime/relay/policy.go:22-37` currently sets runtime, segment,
  rate-share, connection-share and billing authorization fields to `true` for
  the local tracer snapshot.
- `engine/cmd/vortex-relay/main.go:76-111` only constructs `sourceGate` when
  `source_credit_book` or `source_credit` is configured.
- `engine/runtime/relay/bridge.go:319-332` treats a nil Gate as no session
  accounting and continues bridging. Therefore an unconfigured relay can pass
  traffic without funded authorization; this is not closed by finite test
  issuer evidence.

## Accepted Risks Log

No accepted risks.

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-11-05 | 4 | 3 | 1 | Codex in-window review |

## Sign-Off

- [x] Threat dispositions recorded
- [x] Unaccepted open threat remains explicit
- [ ] `threats_open: 0` confirmed
- [ ] `status: verified` set in frontmatter

**Approval:** blocked — T-04-02 requires a fail-closed production authorization boundary before phase advancement.
