---
phase: 06-m4-m4-backend
plan: 05
status: awaiting-owner-contract-approval
gate: blocking-human
implementation_tasks: 3/3
acceptance_complete: false
---

# 06-05 契约批准续接点

CR-006 已正式批准，06-04 已完成。本次不是再次请求 CR-006。
06-05 三项实现和本机完整验证已完成，新增 CR-007 尚待明确批准。
Phase 6 保持 7/11，不创建完成 SUMMARY、不勾选 MTR 需求、不启动06-06。

## 精确审查对象

- 候选：`docs/changes/CR-007-metering-ledger.json`
- SHA-256：`3ddd1ab79c656b3684e64a3854f32785ff2fb8998743ac73353cdc90b086b0c5`
- 中文说明：`docs/changes/CR-007-metering-ledger.md`
- 实现与测量源：`995d7fca563c87cc8c3b38f3252e50b8ab94458e`
- 原始证据提交：`8b4a0d9e`
- 该 JSON 固定20项规范产物及已批 CR-006 基线；历史 CR-005/006 原字节保持。

| 契约 | 变更 |
| --- | --- |
| database | 2.1.0 → 2.2.0，追加00007来源账本、用量、未知负债及足额风险预算 |
| public API | 2.1.0 → 2.2.0，逐来源账本和计费解释；HTTP适配仍06-07 |
| control.v1 | 1.4.0 → 1.5.0，仅协商 usage-ledger-v1 后传原始Node用量身份 |
| runtime / authorization / VLink | 保持既有批准字节与版本 |

批准依据来自 `docs/governance/contract-review.yaml` 的明确约束：
“Any normative change requires owner approval, a SemVer update, requirements-map review and changelog record.”
现有 CR-006 批准仅绑定其精确候选；没有创建 CR-007-APPROVAL.json 或追加新批准。

## 已完成实现

| 任务 | 实现提交 | 内容 |
| --- | --- | --- |
| 1 | 929ba6be（RED）、04260fb1 | 不可变来源/快照、哈希事件链、B/F/L投影重建、来源隔离、24h计费政策候选、计费解释和审计回滚 |
| 2 | 518e522d（RED）、6c754473、36e69cf5 | 原始用量tuple幂等、摘要冲突争议、预留/实耗/退回/最终证明、OpenAPI错误封套 |
| 3 | 981659c8（RED）、577b69a0、23574367 | unknown冻结、持久对账worker、72h争议、明确足额平台预算及独立补偿来源、真实Agent/Hub/PG用量链、候选和证据门禁 |

补查反例与修复：

- `3b3a91df` RED → `577b69a0`：拒绝空条目及收费快照冒充自托管，拒绝时不改变来源权威。
- `56c63092` RED → `0c18aab8`：拒绝未来保护带起点，维持12秒上限和硬截止。
- `33db4c25` RED → `995d7fca`：未知用量PG回执丢失后，Agent再次重启仍复用首个报告boot/sequence并取得原确认；不覆盖身份、不重复扣款、不释放未知负债。

## 最终验证

原始证据目录：`artifacts/phase6/06-05/995d7fca563c87cc8c3b38f3252e50b8ab94458e/`。
最终源来自独立完整克隆，前后均clean；不是调用方混合工作区的测试结果。

| 检查 | 实际结果 |
| --- | --- |
| Linux arm64 Docker，Go1.27.3，真实PG18.6/Valkey9.1.3 | 后端全套普通/race通过，包含两轮真实backend/Agent/Hub进程 |
| Agent、SQLite store、Hub、Engine组件 | 全套普通/race通过，包含新增原始身份跨boot重放 |
| 故障专项 | 实际应用进程SIGKILL/PG连接终止、回执丢失、两次Agent boot、重复报告、24h/72h推进均通过；未知负债不因超时释放 |
| Linux amd64 / arm64 | 两架构二进制及Engine包编译通过；不是原生双架构执行证据 |
| sqlc/Protobuf/OpenAPI/依赖许可 | 两次干净生成一致；已提交字节一致；旧wire不变；历史精确批准和候选hash通过 |
| 实际Cloudflare受控CRUD | exit0，policy摘要与真实操作记录在cloudflare.json/log |
| host manifest | 96项归属、普通/race指定用例、全部35项producer checksum通过 |
| 门禁反例 | 重算checksum后移除保护带/原始身份用例，分别被语义检查拒绝；17项工具检查通过 |
| 契约冻结 | runtime已批字节通过；新增DB/API/control精确owner批准待完成 |

`docker/result.json` 保留 `result=0`、`source_clean=true`、`acceptance_complete=false`。
完整入口返回1的唯一最终原因是待owner契约冻结；没有把入口标为整体通过。
最终同SHA GitHub Actions配对仍归06-11，当前未运行。
SIGKILL和虚拟时间推进不证明物理断电、PG物理磁盘恢复或真实等待72小时。
活跃signer、份额/运行租约及Hub资金委托producer仍由06-06实现；没有合成默认资金、epoch或租约。

早期共享Git对象路径导致构建失败，以及被后续修复替代的两次中断运行，
均保留于`artifacts/phase6/06-05/failed/`，原结果1/137和未完成标记不改写。
development目录只记录混合工作区RED/GREEN，不代替最终clean提交证据。

## 批准后的续接

1. 取得owner对上述精确CR-007的明确批准或修改要求。
2. 批准时保留原候选JSON，另记真实回答和时间；在历史末尾仅追加本次DB/API/control三项批准，更新对应规范及companion哈希。
3. 扩展历史批准验证器以校验该合法追加，运行全部选定freeze、候选、生成/兼容与门禁反例；批准元数据新提交重新运行完整clean Docker入口。
4. 验证通过后创建并提交06-05-SUMMARY.md，才可更新需求/进度至8/11并推进06-06；Actions最终配对仍保留06-11责任。

SDK `state.record-session` 已实际尝试，仍拒绝历史Phase5的`passed_with_scope`。
未改写该历史判定；仅窄同步当前Phase6续接状态和路线图。WINDOWS #1保持open，
06-05来源余额/Valkey重建已有实现证据，最终跨域及配对仍待06-11。

## 自检

已复核最终35项producer checksum、附加门禁反例checksum、全部20项候选规范字节、
上述提交均为HEAD祖先，且CR-007批准文件与06-05完成SUMMARY均不存在。
