# CR-010：Phase 4 合并与实验需求字段迁移

状态：**候选，等待所有者对本文及同名 JSON 的精确批准**。用户已授权准备兼容迁移提案；尚未授权冻结这些新字节。本提案完成前不把合并推到 main。

合并来源 `phase4-runtime-authority-qualification@2cb3ab5326a4339ec4c4e9e2917073ba869fa5bb`；目标基线为 `main@00704144`（包含 origin/main `7b0363c9` 的 Phase 5 闭环）。候选位于 `codex/merge-phase4-main` 的独立 worktree；Phase 6 实现分支不参与本次合并。

## 为什么必须迁移

两条已分叉开发线把 control 1.3.0 的相同字段号分配给了不同消息。直接选择一边会让另一边的消息按错误类型解码。原 Phase 4 分支的批准只绑定其原实验字节，不能批准本次迁移后的契约。

| 消息容器 | 主线已发布字段（保留） | Phase 4 原实验字段 | 本次候选 |
| --- | --- | --- | --- |
| NodeMessage | 16 LifecycleHello；17 NodeReport；18 RenewalRequest | 16 CreditDemand | 100 CreditDemand |
| ControlMessage | 15 HubStatus；16 RenewalResult；17 CredentialRevocations | 15 CreditDemandDenied | 100 CreditDemandDenied |

请求/拒绝消息内部字段与枚举值沿用来源分支。其他已有字段、授权 payload、runtime schema、身份 profile 和生命周期 schema 保持原定义。候选为 **vortex.control.v1 1.7.0**，从 main 1.3.0 作 additive minor 扩展；跳过 1.4..1.6 是为了不复用 Phase 6 分支已经批准的版本。它不导入或宣称实现 Phase 6 的功能，既有 LifecycleHello 仍协商已实现的 1.3.0 status-v1/renewal-v1 子集。后续合并 Phase 6 必须保留该分支全部精确批准扩展并重做兼容核对。

## 运行与迁移约束

- 默认关闭实验需求。Node 显式开启 `EnableCreditDemand` / `--credit-demand-config`，Hub 显式配置 `DemandIssuer`；仅限独立的非区域实验夹具路径。不能与区域生命周期/续领流混用，CLI 也拒绝混合两种夹具参数。
- 停止旧实验夹具，使用独立 endpoint 与状态目录，同时部署迁移后的两端。不提供字段 16/15 的自动转换或旧协议回退。旧请求只可能进入生命周期校验，旧拒绝只可能成为未协商 HubStatus；均不能被解释为实验需求成功/拒绝。字段 100 对旧 peer 是未知消息，不能授予权利。
- mTLS Hub/Node 身份、当前 boot、原 delegation/source/snapshot/rule 绑定继续强制检查。请求只是意图；成功必须先 FULL 提交原资金扣减，拒绝必须先提交“未签发”的终局事实。超时、过期、丢失响应和 ACK 均不退款、不解除未知负债。
- 实验需求路径的速率补充使用已有 boot 绑定的本地单调时钟。每次写入仍验证保守的授权时间边界、租约和原速率上限。时钟切换保留令牌，不因停机增加额度；主线区域路径不启用该实验选项。
- SQLite 合并 schema 标记为 20，仅在 Phase 4 块/需求与 Phase 5 续领/保留/凭据/恢复/政策迁移均成功后发布；重开不回退版本。旧请求/原 payload 保留精确重放语义。自动升级预检只接受 schema 20；从旧 schema 的迁移和旧二进制回退须单独审查，禁止恢复旧金融数据库快照。
- 原 D-04 结论仅限 Phase 4 Engine/tracer 的已测源与配置，不改变生产默认参数，不包含未批准的 grant-return 协议。

## 合并冲突的具体整合

保留主线当前会话 fencing、实时证书吊销、区域 failover、带来源续领、政策快照/份额链、升级/排空/状态命令。叠加来源分支的有界早期 TCP DATA、重放内存上限、持久化需求、块/片段预扣与 checkpoint、资格工具和原始证据索引。共同事务帮助函数接受普通事务或 runtime FIFO 事务，所有被选择的授权仍在同一事务内完整核验；粗筛只跳过已耗尽额度和已选种类，不缓存权限。

## 证据与限制

具体命令、结果、失败与修复见 quick 任务 `261109-n63` 的 EVIDENCE.md 及 `artifacts/merge-phase4-main/report.json`。候选最终源码清单与 SHA-256 见同名 JSON 和 `candidate-source.json`。原 Phase 4 最终审计与 D-04 锁文件保留其历史 SHA；合并候选的回归不冒充新提交的性能资格化。

四项独立 Node/测试 Hub 进程测试已在 Darwin 与 Linux arm64 Docker 通过，包含 TCP、UDP、16 流并发及超过初始额度的持续自动补充，并核对 Agent、Hub、协议后端账本守恒。旧进程夹具已改为统一 runtime 身份 CA、UUID 规则与主线配置激活事务；TLS 正常验证 UUID SAN。实验 Hub 在配置 ACK 后保持需求流，默认非实验行为保留。

本轮本机 Docker、Darwin 辅助测试、交叉构建分别记录；未运行本候选 GitHub Actions，原分支 Actions 不能替代它。冻结检查在批准前应失败。完整 Phase 5 发布验收脚本的 clean-source/冻结前提尚未满足，不声称已重新完成整个 Phase 5 阶段验收。

## 批准的精确动作

批准 **CR-010** 表示批准同名 JSON 列出的 control 1.7.0 及配套字节，允许将 catalog 从 candidate 改为 frozen、在治理记录追加本文/JSON与契约摘要、在 changelog/需求映射记录批准事实，完成 Phase 4 合并并推送 main。原历史批准记录保留。若实际规范字节变化，必须更新候选并重新审阅。
