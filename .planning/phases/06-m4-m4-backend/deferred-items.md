# Deferred discoveries

- `tools/phase1/contracts.py check-frozen` across every contract still reports
  `vortex.vlink.v1: approved review hash is missing or stale`. This predates
  CR-005 and is outside its four approved contracts. All four CR-005 selected
  freezes pass. No VLink approval or historical hash was changed. Retained
  evidence: `artifacts/phase6/06-03/fc63d57406f9f0e3eebe99ad276f95b004add710/all-contract-freeze.log`.
