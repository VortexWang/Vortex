---
phase: 05-m3-agent-edge-hub-m3-agent-and-hub
plan: '14'
status: complete
completed: 2026-11-06
source_sha: deb8b08e5cc8ee4e6f77bc700a4b5b36836c68c1
---

# Plan 05-14 Summary

The Phase 5 consolidated gate now proves the seven required real-process workflows at the Linux runtime boundary. `tools/phase5/check.sh` runs the existing contract, Engine, recovery, authorization, renewal, identity, command, upgrade, catch-up and share/policy checks, then `gap_check.sh` enforces all 31 named process cases, raw SQLite snapshots, source/file hashes, contract digests and independent audit reports. Missing, skipped, empty, changed, forged or incomplete evidence fails closed.

The snapshot auditor uses immutable SQLite reads so WAL snapshots are audited consistently on macOS and Linux. `tests/phase5/test_gap_evidence.py` covers the complete manifest plus eleven rejection/WAL cases.

## Verification

- Native evidence verifier: `python3 -m unittest discover -s tests/phase5` — 11 passed.
- Same-SHA Docker: clean `deb8b08e` checkout, pinned Go 1.27.3 image, 31 process cases, 60 snapshots, 122 gate files — passed.
- Same-SHA GitHub Actions: run `42224211933`, Ubuntu 24.04 amd64, full Phase 5 workflow and deployment lifecycle — passed in 12m46s.
- Independent raw audit: Docker and downloaded Actions `gap-evidence.json` manifests both passed `validate_manifest`; both retain `backend=protocol-test-only` and `production_backend_authority=false`.
- Native macOS process suite: passed; Linux-only upgrade process witness is intentionally skipped locally and is required by the Linux gate, where it passed.

## Scope

G05-01, G05-02, G05-03 and G05-05 are closed within the Agent/Engine/protocol-backend fixture boundary. The requirement audit maps all 34 Phase 5 IDs. HUB-005/006 production backend transport/allocation remains Phase 6. Physical power-loss durability, actual systemd daemon boot, arbitrary source/relay modes and Phase 4 performance qualification are not claimed.

## Deviations from Plan

[Rule 1 - portability bug] The original read-only SQLite URI could not inspect WAL snapshots on macOS. The auditor was changed to `mode=ro&immutable=1` and a WAL regression fixture was added; all evidence tests and both Linux hosts passed afterward.

Total deviations: 1 auto-fixed. Impact: strengthens cross-platform evidence validation without changing product contracts or runtime behavior.

## Self-Check: PASSED

- All seven process workflows have nonempty logs and the exact required named cases.
- Docker and Actions use the same source SHA and independent manifest checks agree.
- Negative evidence fixtures reject absent, changed, skipped, empty, forged and partial inputs.
- Phase-wide verification, validation, security, review, requirement audit and renewed UAT were refreshed without promoting Phase 6 or Phase 4 scope.
