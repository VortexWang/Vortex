# 06-02 evidence

Accepted local producer source: b71daf7d43a0ddaecba1b68efb3c664b8b0362c1.
The clean clone ran check.sh --through-plan 06-02 --produce docker with the
pinned Linux arm64 Go 1.27.3 image and real PostgreSQL 18.6/Valkey 9.1.3.
Docker result=0, source_clean=true; manifest host validation passed. Full normal
and race runs contain 82 PASS lines combined, zero skips and no race warning.
Dependencies, exact license closure, OpenAPI lint/copy, and two independent sqlc
v1.31.1 generations passed. Both real backend/Agent/blocked-Hub process runs
retain their raw logs and checksum-bound reports.

The migration-status file queries the real Goose version table: 0/1/2/3 applied.
The pinned Goose v3.29.0 library performed migrations. A separate Goose CLI is
not installed; no unreviewed CLI dependency closure was introduced for status.

The source tree was exported from the accepted commit; caller Phase 4/5 dirty
and untracked files were preserved. development/ logs are earlier dirty-tree
developer checks, including the upgrade failure fixed in 0f2ec944; they are not
exact-source acceptance evidence. tdd/ contains unchanged real unittest/Go/PG
RED reports and RED_EVIDENCE_OK machine verdicts for both tasks.

No actual Actions run for this source was performed in 06-02. Docker producer
success is separate from paired acceptance; 06-11 must supply matching real
Actions and Docker evidence for its final source. NOD-012 and PRT-004 remain
pending until compiler/real Agent port-task consumers are connected in 06-03/07.
Real Cloudflare CRUD remains 06-10. No performance or full-system claim is made.
