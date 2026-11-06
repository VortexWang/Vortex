---
phase: 05
plan: '09'
status: verified
plan_complete: true
updated: 2026-11-06
---

# 05-09 runtime identity evidence

Current source: `a9aea9d29b35587fa281ff666877b3874d2c1932`.
Full same-source Docker/Actions verification passed. This closes plan 05-09
within the actual static destination runtime and protocol-adapter scope; Phase 5
remains executing and production backend authority remains Phase 6.

## Implementation

- `agent/identity_runtime.go:CredentialManager.RuntimeIdentity` captures the
  durably selected credential under the publication lock. Engine receives only
  certificate/crypto.Signer and an in-memory current-revocation callback.
- `NewManagedRuntimeEngine` / `runtime.NewForAgent` load pinned runtime trust
  without reading stale identity aliases. The actual node installs its durable
  manager and current Agent boot before listener startup.
- Supervisor captures its provider once per handshake, checks both TLS/HELLO
  public keys, clears late certificate callbacks, pins peer UUID DNS and upstream
  membership, and uses the captured key for exporter-bound HELLO. Identity
  rotation preserves Agent boot, node identity and path generation.
- Node enrollment CSR/signing follows the already approved DNS+URI UUID profile
  with digitalSignature and client/server EKU. Legacy DNS-only identities remain
  control-only; runtime rejects them rather than bypassing validation.
- `CredentialRevocations` uses the already approved 1.3.0 message. Authenticated
  current-stream issuer binding, strict bounds, additive monotonic revision,
  exact duplicate payload and FULL-WAL persistence precede in-memory publication.
  Reopen loads cumulative batches; failed persistence cannot publish them.
- Future revocations deny affected identities when conservative Latest reaches
  the boundary or time is untrusted. Missing entries in later batches never
  restore old serials/nodes. Engine callbacks do not access the database.
- New local/remote OPEN checks captured local and peer expiry/current revocation
  before business authorization, leaving existing stream accounting intact.
  Control handshake, active stream and renewal checks reject expiry/revocation.
  TLS session resumption is disabled. Ordinary rotation retains bounded old
  business sessions; it does not refund unknown debt or replace authority.

## Native development checks

Go 1.27.3 Darwin arm64:

- Root and independent Engine `go test -count=1 ./...`: passed before final
  peer-expiry assertion; final complete Linux regressions remain required.
- `go test -race ./agent/...`: passed.
- Engine `go test -race ./runtime/... ./vlink/...`: passed.
- `go test -count=10 -race ./agent -run TestRuntimeIdentity`: passed on the
  earlier source; gate now includes the final peer-expiry assertion as well.
- Root/Engine vet: passed after correcting a protobuf test copy of lock state.
- `TestIdentityProcessRotation`: real shipped node/Hub processes, local fixture
  CA enrollment endpoint, active TCP, live credential renewal, fresh full TLS
  and exporter HELLO using new serial/signer, stable boot/path, and successor
  control connection authenticated with the new serial: passed.
- Negative/finite tests cover DNS-only/SAN/EKU/key usage, unrelated CA, both key
  mismatch directions, expiry/revocation, concurrent snapshot publication,
  current peer revocation, FULL write failure, crash before/after durable
  selection, stale aliases, overlap expiry and no fallback from revoked active.

These are development checks, not Linux full-disk acceptance or production
backend CA/token authority. Test services remain explicitly protocol fixtures.

## Failed and superseded attempts

- `97eb2c2` local Docker full gate failed in root tests: legacy identity test
  expected revocation at handshake; new lifecycle-only peer observation also
  changed an old fixture's generic event count. Frozen contract checks passed.
  Raw failed report/logs retained in `artifacts/phase5/check/97eb2c2/docker/`.
- `a6f686e` / `f341db1` moved local fail-closed admission to CredentialManager
  and isolated identity observations to configured lifecycle test Hubs. Focused
  race regression passed. Full f341db1 runs are historical/superseded by a9aea9d.
- Final peer expiry recheck prevents an already-connected control stream from
  outliving its verified peer certificate. No contract/dependency change.

## Historical pending list before final verification

- Same-source `make phase5-check` Docker arm64 / Actions Ubuntu amd64 outputs,
  exact run metadata, raw evidence and independent accounting audits.
- Retain final coverage and threat mapping in SUMMARY only after these gates pass.
- Full Phase 5 verification/UAT and later 05-10..14 remain separate work.

## Final same-source verification

Source `a9aea9d29b35587fa281ff666877b3874d2c1932` passed complete
`make phase5-check` in clean pinned Go 1.27.3/protoc 36.2 Docker Linux arm64
and [GitHub Actions 42181891508](https://github.com/VortexWang/Vortex/actions/runs/42181891508)
Ubuntu 24.04 amd64. Both required the actual node rotation process tracer,
70 named identity race passes (seven tests x ten), three Engine identity/admission
cases, all prior regional/renewal/scheduler/share/retention races, complete root
and independent Engine tests, frozen contracts, real SQLITE_FULL and recovery.

Raw evidence is retained under
`artifacts/phase5/check/a9aea9d29b35587fa281ff666877b3874d2c1932/`:
`pair.json`, per-host reports/logs/snapshots/run metadata and `files.sha256`.
The Actions archive included 1900 redundant historical entries; only 105
root-level current outputs were selected, recorded with archive digest and exact
file selection. No current output was omitted.

Independent raw audits rerun under pinned Linux Python/SQLite passed on both
sets: five recovery snapshots/ten faults, twelve finite renewal safety snapshots,
and conservative retention/unknown-debt checks. The real rotation process also
checks original funded TCP was not reconnected, original expiry unchanged, source
conservation and a 512-byte unresolved liability retained through rotation and
control failover. No fixture is promoted to production CA/allocation authority.

Task assertions and threats T-05-15/16 have finite executable evidence: profile,
key/coherent snapshot and peer admission rejection; FULL selection/revocation,
reopen/stale aliases, failed publication, revocation/expiry/overlap and preserved
liability. This is tested behavior, not an unbounded proof or performance claim.
Later CLI/drain/upgrade/Hub/backend/share-policy work remains 05-10..14.
