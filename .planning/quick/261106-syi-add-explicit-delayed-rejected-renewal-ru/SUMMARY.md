---
quick_id: 261106-syi
status: complete
---

# Delayed Rejected Renewal Evidence

Added `TestFaultedRenewalRejectedIsDefinitiveAndConserved` to cover the
process-local delayed issuer's definitive rejected renewal outcome. The test
checks bounded denial, conservation, no unknown liability, no fabricated grant,
and wait counters that distinguish rejection from timeout.

Validation passed:

- `cd engine && go test -race ./runtime/accounting -run 'TestFaultedRenewal(Unknown|Rejected)' -count=1`
- `cd engine && go test -race ./runtime/accounting ./cmd/vortex-relay`
- `cd engine && go test -race ./...`
- `python3 -m unittest discover -s tools/phase4 -p 'test_*.py'`

Formal performance qualification, D-04 production parameter lock, and
persistent Agent/Hub authority remain open by design.
