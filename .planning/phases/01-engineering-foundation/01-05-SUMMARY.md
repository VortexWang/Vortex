---
phase: 01-engineering-foundation
plan: 05
status: partial
completed: 2026-11-03
gap_ids: [G-01-01]
---

# Plan 01-05 Summary: Candidate Closure Review

> Scope update (2026-11-03): this review's broad closure gate is now applied just before each dependency's first phase use. PostgreSQL and Valkey are external services, so their OS package license closure is not a product gate; keep their versions and digests recorded. This partial review does not block M0.

Reviewed all seven unresolved candidate categories and recorded evidence in `docs/governance/dependency-closure-review.json`.

Verified evidence:

- SQLite `modernc.org/sqlite` v1.61.1 module checksum, Go module checksum, upstream tag commit, module archive SHA-256, SBOM/license references, and runtime `sqlite_version() = 3.54.1`.
- The repository's `FROM scratch` network fixture rebuilt deterministically from the clean `e2eaf25608eec84f88217529e52a339ab6b4450c` archive for linux/amd64 and linux/arm64. Two no-cache image builds matched per architecture.
- The fixture source imports only the Go standard library and its source blob and architecture-specific build hashes are recorded.
- `@vitejs/plugin-react-swc` registry metadata, exact direct dependency resolution, linux native package integrity metadata, and declared license expressions.
- Docker CLI Go checksum database values and Compose/Buildx source license hashes; known provenance limitations are recorded.

Blocking findings:

- Syft 1.52.0 scans of the locked PostgreSQL and Valkey Bookworm image digests found Debian packages with GPL/LGPL/GFDL license metadata. A PostgreSQL Alpine candidate was also scanned and contained GPL/LGPL packages. Neither base image is compatible with the current permissive-only draft policy.
- The npm native SWC platform license expression `Apache-2.0 AND MIT` is not explicitly in the draft allowlist.
- A standalone Docker CLI build from tag v29.8.2 failed because of ambiguous split `google.golang.org/genproto` imports. OrbStack's signed local CLI proves the OrbStack publisher signature, not Docker upstream provenance.
- The final SBOM tool version 1.56.0 was not successfully installed; the exploratory image scan used Homebrew Syft 1.52.0. Other exact-version application dependency closure also remains incomplete.

No candidate manifest, approval digest, license policy, or version lock was changed. The original approved snapshot remains intact. `baseline.py candidates` therefore remains blocked by the seven unresolved records.

Verification: 12 Phase 1 Python tests pass; GSD consistency passes; candidate validation correctly remains blocked. This plan is partial and does not close G-01-01.
