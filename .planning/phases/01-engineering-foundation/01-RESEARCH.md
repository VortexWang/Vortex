# Phase 1: 工程基线与安全契约 - Research

**Researched:** 2026-11-03
**Status:** Complete
**Scope:** ENGR-001..006, NFR-038 and Phase 1 supplemental requirements.

## Summary

Phase 1 should establish reviewable artifacts before product code: a candidate version/license inventory that the user approves before it becomes a lock, five versioned contracts with complete requirement traceability, a finite TLA+ model checked by TLC, and one Docker-backed test command used by both the developer machine and GitHub Actions. The repository has no product implementation or test infrastructure yet, so the first plan tasks must create the required directories, scripts and evidence formats. This phase can complete its verifiable work while NFR-038 remains blocked by missing historical evidence.

The repository's existing stack research is a candidate catalog, not an approved baseline. A fresh official-source check on 2026-11-03 confirms Go 1.27.3 stable, Node.js 26.13.0 LTS, PostgreSQL 18.6, and TLA+ tools v1.7.4 as current candidates. These exact values, all direct and transitive dependencies, build tools, test tools, action commits and image digests must be re-collected into a complete candidate manifest and presented to the user before any lock file is committed. The final inventory must preserve source URLs, publication/version evidence and SHA-256 values; an unverified or unknown license is a rejection, not an implicit allow.

TLA+ with TLC is recommended over a hand-written deterministic simulator. The lease, quota, activation uncertainty, ledger handoff and immutable billing snapshot rules form a concurrent transition system where state exploration is valuable. Use a bounded finite abstraction and check explicit invariants plus a coverage matrix for NFR-027..045 and selected pairwise faults. The result demonstrates the checked model and bounds only; it is not an implementation proof or system test. TLA+ tools are MIT-licensed and need Java 11+, while this Mac has no Java runtime. Run the same pinned TLC container from Docker locally and Actions instead of depending on host Java.

## Recommended Phase Breakdown

1. **Candidate baseline and governance evidence:** inventory every missing historical artifact referenced by V1.11; record owner/source, search action and evidence link. Produce a complete candidate manifest, request user confirmation, then emit locked versions, provenance, license policy, SBOM and automated baseline checks. Keep NFR-038 blocked if its required historical review/freeze record is absent.
2. **Formal contracts and traceability:** create versioned database, OpenAPI, Protobuf, runtime configuration and VLink contract artifacts; map every requirement ID to a contract or a reasoned not-applicable entry. Add SemVer and mapping/record consistency checks.
3. **ENGR-006 model:** define state variables, transitions, safety invariants, reachable/forbidden states and risk-selected pairwise fault cases. Run TLC in a pinned container and retain machine-readable output and model metadata.
4. **Shared verification entry and CI:** create `make phase1-check`, Docker configuration and GitHub Actions that invoke the exact same command and archive results. Add the network-namespace harness surface for later Engine performance/leak tests without claiming Engine tests pass before Engine exists.

Plans should keep governance/versions, contracts, model and CI concerns separate, with actual file overlap represented as sequential dependencies. A candidate manifest approval checkpoint must precede the immutable lock and any dependency installation. The missing-evidence gate is independent of other artifacts: the check must report NFR-038 blocked rather than fabricate an attachment or pass record.

## Technical Findings

### Version and license baseline

- ENGR-001 requires exact versions for Go, Node.js, every dependency and container image and prohibits floating `latest`. ENGR-002 additionally requires a machine-enforced permissive-license policy, version/checksum/license inventory and release SBOM. Include generators, GitHub Actions, model checker, test-only tools and OS packages in the inventory, not just runtime dependencies.
- As of the review date, official sources identify Go `1.27.3` as stable, Node.js `v26.13.0` as LTS, and PostgreSQL `18.6` as current supported minor. These are candidates only and must be reconfirmed in the user-visible candidate list before lock.
- TLA+ tools `v1.7.4` is the latest stable GitHub release observed; upstream master describes a separate `1.8.0` pre-release. Do not adopt a pre-release as stable. The tools repository is MIT-licensed and its command-line tools require Java 11 or newer.
- Use full 40-character commit SHAs for Actions, immutable image digests, source archive checksums and exact package lockfiles. Unknown licenses or versions without verifiable source/checksum evidence block approval.
- The current Docker/Compose environment was observed as Docker Engine `29.8.2` and Compose `v5.5.0`; pin these as environment evidence, not as application image versions. The GitHub repository connection has write/admin permission, while local `gh` credentials are a separate read-only identity; the workflow must be verified through the authorized VortexWang path.

### Contract validation

- ENGR-004 names five normative files: database, OpenAPI, Protobuf, runtime config and VLink encoding. Treat each as an independent SemVer contract. Preserve requirement and acceptance IDs; every requirement must map to one or more contract paths or a reasoned `not_applicable` mapping.
- CI should compare contract content changes with an explicit version change and an append-only contract change record. Validate that mappings point to existing requirement IDs and existing contract identifiers; reject duplicate/missing IDs and empty not-applicable reasons.
- Runtime defaults and ranges must be sourced from Appendix A, not retyped from research notes. The process-local Engine interface and command surfaces in supplemental requirements also need a normative location and mapping.
- Contract artifacts are public-to-repository interfaces; breaking changes require a major SemVer increment and an explicit migration/compatibility note. A linter cannot infer semantic compatibility in every schema, so human review evidence remains necessary.

### ENGR-006 model

- Required invariants include per-source `F_s <= B_s + L_s`, incremental authorization no greater than remaining source allocation, unique active generation, share-chain handoff occupancy `max(a,b)`, exactly-once ledger transfer `R`, idempotent durable usage, no unfunded release of unknown liability, no resurrection of old generations, immutable billing snapshot isolation and conservative handling of activation-unknown.
- Build a finite abstraction for source counts, quotas, generations, revisions, epochs, authorization delivery/ack outcomes and bounded clocks. Keep the abstract-to-spec mapping explicit; include all NFR-027..045 state families and a risk-ranked pairwise fault table with inclusion/exclusion rationale.
- TLC should check invariants on the configured state graph and export version, bounds/constants, state count, exit status and counterexample path. Include a deliberate negative-model test so the harness proves it detects a violated invariant. Bound explosion by model reduction and document omitted/unbounded behavior; do not claim exhaustive real-valued time, all interleavings, or implementation equivalence.
- Docker is the local and Actions runtime for the exact pinned TLC jar + JRE. Since Java is absent on the host, a host-Java command is not a valid local test path.

### Shared local and Actions verification

- Use one repository command (`make phase1-check`) as the interface for both local Docker and Actions. It should run formatting/static checks, requirement/contract/governance validators, TLC, and artifact collection with clear non-zero exits.
- Pin all workflow actions by full commit SHA and all container images by digest. Prefer a stable Ubuntu 24.04 runner label; record the actual runner image/architecture in artifacts. The runner executes Docker to invoke the same test image/command used locally.
- Persist machine-readable reports and raw TLC output as Actions artifacts; record commit SHA, dirty state, host/runner, architecture, image digest, tool versions, test command and exit result. Local and Actions results are separate evidence records; neither implies the other passed.
- Establish the Linux network-namespace harness and a fixture smoke check now. Actual Engine performance/leak assertions are added once M0 provides an Engine test binary and must not be marked passed in Phase 1.

## NFR-038 Evidence Gap

The repository currently contains the normative V1.11 file and CR-001, but not the historical freeze/review records and checker materials cited by the source. Inventory exact references by scanning the V1.11 body and appendices, verify each path against the working tree and remote repository through the authorized connection, and record search scope, owner/source and result. Preserve a machine-readable `blocked` status for NFR-038 until the original evidence is retrieved and verified or the requirement is formally changed. No generated Phase 1 review can be represented as the missing historical review.

## Validation Architecture

`make phase1-check` is the single verification interface. Wave 0 creates the test entry point and pinned Docker execution environment; until then the command is expected to be a missing-reference sentinel, not a passing test. The command should run requirement mapping and contract validators, governance evidence status checks, TLA+ parsing/model checking, network namespace fixture smoke tests, and artifact metadata validation. Run it inside the same digest-pinned container from local Docker and the GitHub Actions runner. It must exit non-zero for missing mappings, empty not-applicable reasons, contract content changes without SemVer/change records, unapproved or unknown license/version entries, TLC invariant violations, absent result artifacts, or an incorrect NFR-038 pass claim.

Quick feedback should run the Python standard-library validators and a small TLC model under 30 seconds. The full check should enumerate the approved finite model and run the namespace harness, with a target below five minutes on the local Docker VM and hosted runner. Archive complete TLC output and JSON summaries. The user must review and confirm the exact candidate baseline before lock; the owner must manually review that each NFR-038 reference was searched and that the report accurately remains blocked. A green bounded model is necessary but insufficient for Phase 1 acceptance.

## Official Sources

- Go downloads API: https://go.dev/dl/?mode=json
- Node.js release index: https://nodejs.org/dist/index.json
- PostgreSQL version support: https://www.postgresql.org/support/versioning/
- TLA+ tools releases: https://github.com/tlaplus/tlaplus/releases
- TLA+ command-line usage and license: https://github.com/tlaplus/tlaplus/blob/master/README.md and https://github.com/tlaplus/tlaplus/blob/master/LICENSE
- GitHub Actions security hardening (full-length action SHA pinning): https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions
- Docker multi-platform builds: https://docs.docker.com/build/building/multi-platform/

## Confidence and Open Risks

- **High:** V1.11 and ENGR source requirements; user decisions in `01-CONTEXT.md`; current Docker availability/version; Go, Node and PostgreSQL candidate release metadata; TLA+ upstream license and Java requirement.
- **Medium:** TLA+ abstraction adequacy until a reviewable state model and coverage matrix exist; license scanner completeness across Go modules, npm, generators and OS packages.
- **Open:** exact transitive package versions, checksums, image digests, GitHub runner availability, historical review/freeze evidence, actual Actions execution and model state-space size.
- This research used official release APIs and upstream documentation after the configured web search endpoint returned 404. No local project tests or product code have run.
