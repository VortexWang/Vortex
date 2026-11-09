# CHECKPOINT RESOLVED

用户已明确回复“批准 CR-006”，精确 JSON SHA256：
`60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d`。
2026-11-07T20:45:23Z 记录真实批准，原 proposal 不变，DB/API2.1.0
冻结，CR005 历史条目保持。批准提交 `9db5da0e`。
批准后 clean clone 完整 Docker producer exit0，result0、source_clean=true、
acceptance_complete=true（本机范围）；所有必要普通/race、冻结、生成、
Cloudflare 和门禁反例通过。证据见同名9db5目录。无待用户输入的阻塞。
最终 Actions 配对仍归属06-11，资金/租约与HTTP仍归属后续计划。

以下保留原待批准 checkpoint 作为历史，不代表当前状态。

---

# CHECKPOINT REACHED

**Type:** human-verify
**Gate:** blocking-human
**Plan:** 06-04
**Progress:** 3/3 implementation tasks and runtime verification finished; plan acceptance pending
**Status:** awaiting-owner-contract-approval

## Latest Safety Verification Addendum

最新测试源为 `a9275d8660d5041cb9d7d4b7ab4e22c3095624ec`。下文 d7e83d76
表格保留为前轮历史；本轮新证据位于
`artifacts/phase6/06-04/a9275d8660d5041cb9d7d4b7ab4e22c3095624ec/`。

父代理要求的实库负例已证实并修复：节点 offline / grant revoked 会使原先
到期的 disabled revision 激活校验失败，回滚到期限制/来源到期/保留时钟，
并阻塞其他租户。RED `e81835b8` 保存真实失败；GREEN `a9275d86` 仅允许
严格相同既有修订的 Enabled=false 转换复用已存依赖，停止意图不再依赖
在线或仍持有激活授权。修改其他字段仍完整 resolve，实际执行保持 pending。

新增 `TestPackageLifecycleExpiryIndependentOfNodeAuthority` 验证两种失效、
到期权威、来源到期、七天保留、unknown occupation、其他租户推进、依赖
来源一致、无虚构 active，及 disabled 改目标不能绕过当前权限。
本机全backend普通/race通过 13.965s/19.098s；独立 clean clone Docker
全backend普通/race通过8.998s/15.116s；完整组件普通/race、sqlc两生成、
Linux双架构编译、真实Cloudflare CRUD4.74s与host manifest通过。
删新增测试的反例被门禁拒绝。源clone始终clean。

CR-006 全部规范字节和精确 JSON 哈希保持不变。result=0、
acceptance_complete=false，入口仅在待批准freeze gate最终exit1；未推进
SUMMARY/STATE。旧失败与旧成功证据均保留，未冒充为新源结果。

## Completed Tasks

| Task | Name | Commits | Key files |
| --- | --- | --- | --- |
| 1 | Immutable catalog, subscription, source and billing snapshots | 3e55bdfd; a5d3ba08; a071c601 | migration00006, packages/subscriptions SQL, package.go, schema.sql, migration preservation test |
| 2 | Atomic entitlement redemption including cross-package confirmation | 7de2edf1; b594a877; 6a66412a; 9d81cc22; d7e83d76 | entitlement.go, entitlement_confirmation_test.go, generated SQL, supported RED evidence |
| 3 | Restriction projection, future preview/confirm, monthly and expiry workers | e87dce38; b47724ba; a0188f13; 9ff95f0d; f2d9641b | restriction/lifecycle services and tests, durable package worker, exact contracts and producer gates |

## Current Task

**Task 3 final contract verification:** approve or reject the exact CR-006 candidate.
**Status:** awaiting verification.
**Blocked by:** `docs/governance/contract-review.yaml:66` requires:
“Any normative change requires owner approval, a SemVer update, requirements-map
review and changelog record.” The implementation/continue authorization does not
approve exact normative bytes. CR-005 approval remains unchanged and applies only
to its own exact review unit.

## Concrete Review Unit

- JSON: `docs/changes/CR-006-packages-entitlements.json`
- SHA-256: `60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d`
- Chinese review: `docs/changes/CR-006-packages-entitlements.md`
- Baseline: owner-approved CR-005 freeze `fc63d57406f9f0e3eebe99ad276f95b004add710`.
- DB and public API: compatible 2.0.0 → 2.1.0 candidate; control/runtime/authorization/VLink normative bytes unchanged.
- All normative current/baseline artifact hashes are in the JSON. Historical CR-005 proposal/approval/governance bytes are preserved.

Cross-package code preview/confirmation now succeeds. Confirmation locks and
consumes the code with redemption, immutable grant snapshot, future change,
operation/task/outbox/audit in one transaction. Digest pins code hash, actor,
package version, complete impact and explicit time. Code duration/amount are
stored grant terms; catalog capability, price and reset policy remain versioned.
Future due execution rechecks actor and impact, without early activation.

**Owner-visible business decision in this candidate:** a code is consumed on
successful confirmation. If future application later fails due to revoked actor
or stale impact, the accepted consumption remains auditable and is not silently
refunded or reissued. Same-key replay returns the stored operation; changed
code/actor/inputs conflict. Review this semantic explicitly when approving.

## Verification

Tested source: `d7e83d76b7ce6e0687ff66c6d6d60a4cef2b9523`.
Evidence root: `artifacts/phase6/06-04/d7e83d76b7ce6e0687ff66c6d6d60a4cef2b9523/`.

| Check | Result |
| --- | --- |
| Native Go1.27.3, PostgreSQL, backend all packages ordinary/race, TZ=UTC | Passed; service 13.619s / 18.228s |
| Exact-commit clean-clone Linux Docker all backend ordinary/race | Passed; service 8.825s / 16.923s; before/after git status empty |
| Real backend/Agent/Hub processes | Passed ordinary/race; unknown occupation retained, Hub blocked without synthesized signer authority |
| Full Agent/Engine/contracts ordinary/race components | Passed; no skips or race reports |
| Linux amd64/arm64 backend/Agent/Engine compilation | Passed; no native dual-architecture performance claim |
| sqlc v1.31.1 two independent generations and committed output | Passed; pinned dependency/license closure passed |
| OpenAPI source and copy lint/cmp | Passed; 16 authenticated business operations |
| Exact CR-005 historical, current unchanged wire/runtime, CR-006 hashes | Passed; DB/API owner freeze pending |
| Fresh migrations and seeded00005→00006 preservation | Passed; old migration bytes and authoritative/unknown observations retained |
| Goose v3.29.0 PostgreSQL-only local runner up/status | Passed; six migrations applied, source/build metadata retained; not claimed as upstream full-driver CLI |
| Actual Cloudflare create/read/update/delete | Passed, 4.36s; TTL60/DNS-only, token/policy identity and exact-name cleanup |
| Docker host manifest | Passed; 96 owners and required tests ordinary/race |
| Gate negative controls | Pending-owner pairing rejected; missing new confirmation test rejected; checksum corruption rejected |

Docker result records `result=0`, `source_clean=true`, Cloudflare passed and
candidate validation passed. `acceptance_complete=false`; check.sh finally exits
1 at the explicit pending-owner freeze gate. This is not a total acceptance pass.
GitHub Actions was not run in this continuation; final same-commit Docker/Actions
pairing remains 06-11.

The package worker persists stop revisions. Actual execution is handled by the
existing rule compile/deploy/Agent chain, retaining unknown evidence. Production
funding and signer/path/lease producers remain 06-05/06-06; compile requests wait
for real inputs. No default epochs or funds are invented to claim a completed
funded deployment. HTTP business adapters remain 06-07.

## Deviations and Retained Failures

- Clean source ad00a4b4 correctly failed because the regenerated schema review
  copy was missing from its commit; a071c601 committed the exact regenerated copy.
- Clean Linux source a071c601 exposed a replay equality failure: PostgreSQL
  timestamps carried a different Go location object from JSON replay despite
  identical instants. Actual TZ=UTC PostgreSQL RED passed the machine classifier;
  d7e83d76 canonicalized UTC results. Tests and checks were not relaxed.
- The upstream Goose CLI was not installed; a temporary PostgreSQL-only runner
  used the already reviewed v3.29.0 library with no new dependencies and ran
  identical up/status APIs. The source and build/module checksum are retained.
- `failed/` stores failed attempts without changing their result flags.
- Existing untracked task3-red.json was read and retained without overwriting.
- No blocking stubs or new threat surfaces outside the plan trust boundaries were found.

## Awaiting

The owner should approve **CR-006 at the exact JSON SHA-256 above**, or request
specific contract/consumption-policy changes. No CR-006 approval record was
created. After approval, record only that exact decision, freeze DB/API with the
proper approved hashes, extend historical-approval validation for that legitimate
append, rerun selected contract/freeze checks, then create 06-04-SUMMARY.md and
advance Phase 6 state. Until then no completed SUMMARY, requirement completion,
or plan-counter advance is permitted.

## Self-Check: PASSED

Referenced candidate, review, checkpoint and evidence files exist; all task and
continuation commits named above are ancestors of HEAD. Candidate hash matches
the exact review unit, source clone remains clean, and runtime/evidence results
retain their actual pending freeze state.
