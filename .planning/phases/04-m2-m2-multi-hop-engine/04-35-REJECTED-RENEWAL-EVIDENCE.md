# Delayed rejected renewal evidence (2026-11-06)

The process-local qualification issuer now has an explicit `rejected` renewal
fixture alongside the existing `unknown` fixture. The test consumes the first
finite grant, delays the renewal response by 40 ms, and returns a definitive
non-issuance outcome.

`TestFaultedRenewalRejectedIsDefinitiveAndConserved` verifies that the source
reserve fails closed with `ErrDenied`; the pending request is resolved back to
`unissued`, no `unknown_grant` is created, no accepted grant is fabricated, and
the budget invariant still holds. Wait accounting distinguishes the renewal
failure from timeout and preserves the cold-start wait as a separate baseline.

Validation:

```text
cd engine && go test -race ./runtime/accounting -run 'TestFaultedRenewal(Unknown|Rejected)' -count=1
cd engine && go test -race ./runtime/accounting ./cmd/vortex-relay
python3 -m unittest discover -s tools/phase4 -p 'test_*.py'
```

This is an injected, process-local authority result. It does not prove network
issuer behavior, persistent Agent/Hub authority, formal three-trial
performance, or a locked D-04 parameter set.
