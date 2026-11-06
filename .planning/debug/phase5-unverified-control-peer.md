---
status: resolved
trigger: Phase 5 retroactive security audit
execution: inline-owner-preference
---

# Unverified TLS certificate used as control identity

Hypothesis: TestServer.Connect trusts PeerCertificates without VerifiedChains; a permissive listener accepts a certificate issued by an unrelated CA.

Reproduction: TestControlHubRejectsUnverifiedPeerChain uses a real TLS 1.3 gRPC listener with RequestClientCert and a separate-CA client certificate while retaining valid server verification. Before fix, Connect succeeded and the test failed with “untrusted client certificate accepted as control identity”.

Resolution: Require TLS 1.3 and a verified certificate chain at the control handler before reading identity or writing receipts. The caller-supplied listener configuration is not sufficient proof.

Validation: targeted race run covers hostile peer chain, normal mTLS config apply, and certificate/node mismatch. All pass after fix. Final paired Docker/Actions evidence follows the source commit; no production Hub authority is claimed.
