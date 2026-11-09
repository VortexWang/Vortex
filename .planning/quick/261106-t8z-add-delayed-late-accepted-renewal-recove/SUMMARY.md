---
quick_id: 261106-t8z
status: complete
---

# Delayed Late Accepted Renewal Recovery

Added `TestDelayedIssuerLateAcceptedRenewalSettlesOnce`. It proves that a
200 ms caller timeout does not release a pending renewal, that a 300 ms late
accepted reply settles exactly once, and that the resulting credit remains
consumable while conservation holds.

Validation passed:

- `cd engine && go test -race ./runtime/accounting -run 'TestDelayedIssuer(LateAccepted|Timeout)' -count=1`
- `cd engine && go test -race ./...`
- `python3 -m unittest discover -s tools/phase4 -p 'test_*.py'`

Formal performance qualification, D-04 parameter lock, and persistent
Agent/Hub authority remain open.
