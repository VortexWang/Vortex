---
phase: "7"
slug: "m5-m5-frontend-and-operations"
status: draft
nyquist_compliant: false
wave_0_complete: false
created: "2026-11-10"
---

# Phase 7 — Validation Strategy

> 研究后的验证草案。未执行安装、运行时测试或灾备演练；计划生成后补齐Task/Plan/Wave/Threat映射，签署前不宣称Nyquist合规。

## Test Infrastructure

| Property | Value |
| --- | --- |
| Framework | 既有Go testing/race、真实PG集成、Ruby OpenAPI与Python治理检查；拟新增Vitest/RTL/Playwright |
| Config file | 前端与阶段7配置尚缺，Wave 0建立 |
| Quick run command | `ruby tools/phase6/openapi_validate.rb contracts/openapi/v1/openapi.yaml`（现有静态检查，仅覆盖接口结构） |
| Full suite command | `sh tools/phase7/check.sh --suite all`（拟新增，当前不可运行，须有无测试/缺环境失败门禁） |
| Estimated runtime | 尚未测量；快速反馈目标30秒，首次基线实测记录，完整故障/恢复不受30秒目标限制 |

## Sampling Rate

- 每任务提交后运行受影响的单元/组件测试、类型检查及生成校验；不得用现有OpenAPI静态检查替代功能验证。
- 每wave运行隔离真实PG、迁移前后数据保留、API/浏览器及相关Agent/Hub/Engine tracer。
- 阶段验收前，同一clean SHA在本机Docker及Actions原生amd64/arm64分别保存适用的正常/race/浏览器/故障/恢复证据。
- 连续三任务不得缺自动化反馈；缺依赖、零匹配测试、跳过、无测试或恢复环境缺失均不计通过。
- 快速反馈目标<30秒，待实测；备份物理恢复与六步对账属于长时专项，独立测量RPO/RTO。

## Per-Task Verification Map

当前下表是需求到拟建测试的映射。Task/Plan/Wave及唯一Threat Ref待计划生成后依据真实任务填入；因此本文件保持draft。

| Req ID | Behavior / 关键反例 | Test Type | Automated Command / target | Exists? |
| --- | --- | --- | --- | --- |
| FE-001 | 中文UI、错误、空值、日期、数字 | component/browser | pnpm --dir frontend test --run chinese-ui | W0 |
| FE-002 | 仅列表按钮创建、抽屉/完整页 | browser | pnpm --dir frontend exec playwright test creation | W0 |
| FE-003 | 列顺序及真实入口字段 | component/API | pnpm --dir frontend test --run rule-list | W0 |
| FE-004 | 五层/部分/unknown，accepted不变applied | API+browser | pnpm --dir frontend exec playwright test rule-facts | W0 |
| FE-005 | 0～3、方向权益、无许可直调拒绝、手机编辑 | integration/browser | pnpm --dir frontend exec playwright test rule-path | W0 |
| FE-006 | 真实分段测量、无值/过期非0 | integration/browser | pnpm --dir frontend exec playwright test segment-probe | W0 |
| FE-007 | 预览分类/会话水位、冲突重预览 | API/browser | pnpm --dir frontend exec playwright test impact-preview | W0 |
| FE-008 | 中文建议、分段错误、request_id | component | pnpm --dir frontend test --run errors | W0 |
| FE-009 | 损失确认与版本/step-up复核 | integration/browser | pnpm --dir frontend exec playwright test irreversible | W0 |
| FE-010 | 四类通知、归并/恢复/不可关闭 | worker/browser | pnpm --dir frontend exec playwright test notifications | W0 |
| FE-011 | 上报→统计→渲染P95及stale | real process/browser | sh tools/phase7/check.sh --suite freshness | W0 |
| FE-012 | 任一区域降级持续banner，API拒写 | real process/browser | pnpm --dir frontend exec playwright test degradation | W0 |
| SEC-001 | mTLS与公网Bearer边界 | integration | go -C backend test ./internal/httpapi -run TestPublicTLSListenerBearerBoundaryAndShutdown -count=1 | 已有基础，补全矩阵 |
| SEC-002 | 吊销后的重连/续期/OPEN | real process | sh tools/phase7/check.sh --suite revocation | W0；既有身份测试可复用 |
| SEC-003 | 5角色、跨租户、字段/后台任务/节点范围 | API+browser | sh tools/phase7/check.sh --suite authorization | W0；现policy/scope复用 |
| SEC-004 | DB密文、日志/trace/artifact无秘密 | integration/static | sh tools/phase7/check.sh --suite secrets | W0 |
| SEC-005 | 平台TOTP、租户可选、恢复码单用、step-up撤销、限流退化 | API/browser | pnpm --dir frontend exec playwright test account-security | W0 |
| SEC-006 | pending/dispute/F/恢复证据清理不动 | integration | sh tools/phase7/check.sh --suite retention-holds | W0；Agent保留基础可复用 |
| SEC-007 | 每日base、持续WAL、低流量/归档失败 | physical integration | sh tools/phase7/check.sh --suite backup | W0 |
| SEC-008 | 不同凭据、加密、真实解密、错误密钥拒绝 | physical integration | sh tools/phase7/check.sh --suite backup-keys | W0 |
| SEC-009 | 业务标记测RPO/RTO/自治原截止 | full drill | sh tools/phase7/check.sh --suite disaster-recovery | W0；非<30秒套件 |
| SEC-010 | 旧PG余额不重发、来源未清保持F | real process | sh tools/phase7/check.sh --suite recovered-funds | W0 |
| SEC-011 | 全解析/每建连/私网例外/硬禁端口、TCP+UDP全部路径 | unit+real network | sh tools/phase7/check.sh --suite destination-policy | W0；helper现有 |
| SEC-012 | 根离线、中间CA加密、恢复后有效签发 | integration/ops | sh tools/phase7/check.sh --suite pki-recovery | W0；根离线托管需受控人工见证 |
| SEC-013 | 30/90/365及终态边界、分批幂等 | integration | sh tools/phase7/check.sh --suite retention-policy | W0 |
| SUP-PAGES-001 | 覆盖矩阵每页每角色真实API，无mock成功 | browser/coverage | sh tools/phase7/check.sh --suite page-api-coverage | W0 |
| SUP-RECOVERY-001 | 六步、原R立即续签、K与尾部、逐租户放行 | physical+real process | sh tools/phase7/check.sh --suite six-step-recovery | W0；不可用model算例替代 |

所有W0条目目前均pending，不是已有可运行命令。SEC-001现有测试仅基础，仍需扩展公网/控制/VLink矩阵。

## Wave 0 Requirements

- [ ] 明确UI-SPEC生成、检查与前端安全门禁；读取32项决策，不重新解释已锁选择。
- [ ] 经具体版本/许可与安装批准后建立前端manifest、frozen lock、测试/浏览器配置与工具镜像；解决TS7/生成器、SWC待锁问题。
- [ ] 新增阶段7统一验证入口、按套件选择、零测试/缺环境失败与同源SHA证据检查。
- [ ] 新增中文真实登录、最小规则闭环及跨租户/未知/冲突fixture，不能用issue-session代替最终登录验收。
- [ ] 新增隔离PG/Valkey/Hub/Agent/Engine网络与PG18耐久卷；物理备份存储及generation权威独立于被恢复PG。
- [ ] OpenAPI唯一源/副本/路由/真实响应与TS生成两次无差异检查。
- [ ] 覆盖27项要求、32决策、每角色每页面与API/任务/证据关系；每个验证命令有对应失败信号。
- [ ] 对现有阶段4/5未提交改动不清理；实现证据使用独立clean checkout且不混用实现SHA与文档SHA。

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
| --- | --- | --- | --- |
| 根CA离线保管、密钥与恢复凭据分离保管 | SEC-008, SEC-012 | 测试配置无法证明真实托管制度 | 审阅受限恢复资料并见证受控解密/中间CA恢复签发，证据不含秘密 |
| 中文页面和手机真实操作体验 | FE-001, FE-005, SUP-PAGES-001 | 自动浏览器断言不能替代人工体验验收 | 检查列表列序、三栏状态、纵向路径、确认步骤、通知/降级及键盘触达 |
| 真正部署资源和恢复放行 | SEC-009, SUP-RECOVERY-001 | 宿主资源与运维准入需人审阅 | 审阅恢复环境、时间起止点、数据标记、未知负债/自治截止原始记录和逐租户放行 |

## Validation Sign-Off

- [ ] 计划已生成且全部Task/Plan/Wave/Threat映射落地
- [ ] 所有任务有automated验证或明确Wave 0依赖，且每命令具fails_when
- [ ] 没有连续三任务缺自动化反馈
- [ ] Wave 0覆盖全部MISSING引用且已执行
- [ ] 无watch模式，无零测试、skip或缺环境伪通过
- [ ] 实测记录反馈时延和长时专项耗时
- [ ] nyquist_compliant: true仅在证据充分后设置

**Approval:** pending
