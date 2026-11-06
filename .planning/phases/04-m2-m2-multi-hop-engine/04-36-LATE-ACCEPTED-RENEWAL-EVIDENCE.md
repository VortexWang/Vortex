# Delayed late-accepted renewal evidence (2026-11-06)

`TestDelayedIssuerLateAcceptedRenewalSettlesOnce` exercises the caller timeout
boundary with a 300 ms process-local issuer delay. The first renewal attempt
times out at the existing 200 ms bound while its 90-byte request remains
pending. The delayed accepted reply then settles that exact request once.

The test verifies that the late reply produces one additional grant, no unknown
liability, and reusable available credit. A subsequent one-byte reserve and
commit succeeds without increasing the grant record count, and the conservation
invariant remains true.

Validation:

```text
cd engine && go test -race ./runtime/accounting -run 'TestDelayedIssuer(LateAccepted|Timeout)' -count=1
cd engine && go test -race ./...
python3 -m unittest discover -s tools/phase4 -p 'test_*.py'
```

The same commit also passed `DOCKER_CONTEXT=orbstack make phase4-m2-check`.
This is a protocol/runtime regression result, not a dedicated-host performance
qualification.

This remains injected process-local evidence. It does not establish a network
issuer, durable Agent/Hub receipt, formal performance qualification, or a locked
D-04 parameter combination.
