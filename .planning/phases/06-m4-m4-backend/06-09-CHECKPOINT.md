---
phase: 06-m4-m4-backend
plan: 09
status: resolved
source_commit: c1e6b7663a02e65ec9b6b9965c00554ca35fad99
evidence_commit: bbbac76d
completed_tasks: [1, 2]
pending_tasks: []
resolved_source_commit: a948158446255e2eb2e2a975c435ddbb2f357ecf
resolved_actions_run: 42316152923
resolved_evidence_commit: 32698c0d
resolved_summary_commit: 843d12f6
---

# 06-09 authentication checkpoint

Resolved after user browser authentication restored VortexWang write permission.
Both tasks and TEN-009 are complete. Final source a9481584 passed local Docker,
actual Actions 42316152923 and both unchanged same-SHA paired acceptance commands.
Complete SUMMARY is committed in 843d12f6; Phase 6 is 3/11, next 06-02.

Two actual CI failures were preserved and fixed: c1e6b766 omitted already-reviewed
Darwin-only dependencies on Linux; 3c877d4d then hit container Git ownership during
VCS stamping. The final source fixes the reviewed union and scopes container trust
to its source directory, retaining expected SHA and VCS stamping. Raw failures and
successful paired evidence are under `artifacts/phase6/06-09/`.

The original checkpoint record below preserves what was known before resolution.

## Built and verified

- c065bf69: sole OpenAPI source, verbatim backend copy/README, Ruby lint, 96-row
  requirement ownership register, exact-source check/manifest and full-SHA Actions.
- 42679d2f: TDD RED. Real PostgreSQL executed TestRestrictionAllCauses and failed
  after the final clear: operations count=5, expected 7. SDK returned
  RED_EVIDENCE_OK; no fixture/compile/discovery failure authorized GREEN.
- 20acf557: GREEN. The last cause clears in the same AuditWriter transaction as
  recovery desired revision, operation, task and Outbox. Only current ownership
  and valid grants qualify. Pending/unknown execution remains held; historical
  clear replay cannot issue another recovery. Future clear times are rejected.
- affb9c04, 1882e91f, c1e6b766: explicit process file index/checksums, separate
  normal/race report requirements, suspended/banned account negative tests,
  runtime/secret route/schema lint, and before/after source integrity checks.
- Final source c1e6b766 passed the complete clean-clone Docker producer: exact
  Go 1.27.3; full dependency/license closure; two independent sqlc generations;
  all backend normal/race tests; real backend/Agent/blocked-Hub processes; PG
  migration/authority tests; all-cause recovery and final-audit rollback tests.
  Docker is native Linux arm64 under OrbStack. Native amd64 is not claimed.
- 12 rejection experiments passed: wrong SHA, dirty/untracked source, future
  producer, output inside source (no directory created), six malformed OpenAPI
  cases, actual log tampering, and actual missing Actions result.

Evidence is committed under `artifacts/phase6/06-09/c1e6b766/` in bbbac76d.
`docker/result.json` reports result=0/source_clean=true. The paired command
actually failed with `manifest failed: actions result.json missing`. The partial
host producer message explicitly says pairing pending; it is not final acceptance.
The full 96-item register assigns future owners but requires no future producer
files through 06-09. Future implementations must extend the producer explicitly.

## Authentication gate

`gh api repos/VortexWang/Vortex` reports nodeox has pull=true, push=false.
The actual `git push --dry-run origin HEAD:refs/heads/codex/phase6-backend` failed:

```text
remote: Permission to VortexWang/Vortex.git denied to nodeox.
fatal: unable to access 'https://github.com/VortexWang/Vortex.git/': The requested URL returned error: 403
```

Only nodeox is stored; there is no stored VortexWang account to switch to. No fork,
main-branch merge or fabricated Actions evidence was used. The human step is to
authenticate VortexWang or another account with push permission through the GitHub
browser login that the parent agent opens. The agent runs the CLI; the user only
performs the browser authentication. Suggested agent command:

```sh
gh auth login --hostname github.com --git-protocol https --web --scopes repo,workflow
gh api repos/VortexWang/Vortex --jq .permissions.push
```

The authentication gate comes from the executor authentication-gates protocol
and the observed target-repository 403, not an additional approval requirement.

## Agent continuation

1. Verify the restored account's repository push permission without printing tokens.
2. Push the exact verified SOURCE commit to the authorized branch. Its on-push
   workflow can run even though the new workflow is not yet on the default branch:

   ```sh
   SOURCE_SHA=c1e6b7663a02e65ec9b6b9965c00554ca35fad99
   git push origin "$SOURCE_SHA:refs/heads/codex/phase6-backend"
   gh run list --workflow phase6.yml --branch codex/phase6-backend --json databaseId,headSha,status,conclusion
   ```

   Select a real run whose headSha equals SOURCE_SHA; watch its actual conclusion.
   Never replace this with fixtures or relax the paired gate. Do not force-push
   over any intervening branch changes. Metadata/evidence commits are descendants
   of this source; do not relabel their HEAD as the already-tested source SHA.
3. Download artifact `phase6-$SOURCE_SHA` from that run to an external pair directory's
   `actions/`, and copy the committed local `docker/` evidence to its `docker/`.
   Retain run URL, workflow logs and actual result. If Actions fails, keep it failed
   and fix only the directly caused issue under normal deviation rules.
4. In a clean clone checked out at SOURCE_SHA, use pinned Go 1.27.3/local toolchain:

   ```sh
   ./tools/phase6/check.sh --through-plan 06-09 --expected-sha "$SOURCE_SHA" --evidence-dir "$PAIR"
   ./tools/phase6/evidence_manifest.sh --through-plan 06-09 --expected-sha "$SOURCE_SHA" --evidence-dir "$PAIR"
   ```

5. Only after both pass: persist Actions evidence, resolve WINDOWS item 4, write
   the canonical 06-09-SUMMARY.md with measured ledger actuals and self-check,
   mark TEN-009 complete and advance execution to 06-02 (dependency order, not 10).
   Keep TEN-002, source-balance debt 1 and later domains pending as applicable.

## State and safety

Original Phase 4/5 dirty/untracked status was byte-identical before and after
implementation. No blanket staging/reset/clean/stash was used. Root remained
`/Users/peng/Documents/123/VORTEX`, branch `codex/phase6-backend`; isolation none.
Applied migrations and generated query APIs were reused without alteration.
Plan-head ledger is `.git/gsd-plan-head-before-06-09`, originally e2b56e51.

SDK state.record-session still rejects Phase 5's preserved passed_with_scope
verdict, so the current checkpoint/session was synchronized narrowly by hand.
WINDOWS item 4 records the unrun Actions/paired verification. Phase 5's verdict
and Phase 4/8 qualification boundaries are preserved.

## Self-Check: PASSED

All named source/evidence/checkpoint files exist; all seven implementation,
TDD and evidence commits are ancestors of HEAD. Final source identity and local
producer checksums were verified from the clean clone. At the original checkpoint
Actions remained unrun; the resolution above records actual final success.
