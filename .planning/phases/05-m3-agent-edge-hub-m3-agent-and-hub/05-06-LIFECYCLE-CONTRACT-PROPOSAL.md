# 05-06 生命周期契约精确提案

**状态：待项目所有者审批；未冻结、未实施规范变更。**

执行入口：`$gsd-execute-phase 5 --gaps-only`，Wave 5 / 05-06 Task 1。此提案为 Task 2 决策检查点的审阅单位。执行计划的批准不等于批准本提案；已有授权/control 提案审批不覆盖本次增量。已有冻结契约和审批记录保持原样。

## 申请批准的版本

| 契约 | 当前 | 候选 | 兼容方式 |
|---|---|---|---|
| vortex.control.v1 | 1.2.0 | **1.3.0** | 原 RPC/字段编号及类型保留；新增消息和独立 HubControl 后端协议适配器；每条当前流协商能力后启用 |
| vortex.authorization.v1 | 1.1.0 | **1.2.0** | 保留完整 1.1.0 校验分支；1.2.0 分支新增明确份额链、续额关联和策略切换；不向旧严格解析器发送新版本 |
| vortex.runtime-config | 1.0.0 | **1.1.0** | 保留旧有效配置；新增可选区域连接、探测和生命周期调度配置；只有已协商支持的接收端可启用 |

这是能力协商条件下的兼容扩展，不宣称旧解析器能读新字段。对不支持新生命周期能力的节点继续保持旧边界；不得剥离新限制后降级重签。旧运行证书只有 DNS SAN 时仍仅供旧控制连接使用；启用 identity-v1/VLink 前通过本地 CSR 续发双 SAN 证书。若部署不满足这些条件，拒绝启用或阻止自动回退，不修改原冻结字节以迁就它。

## 完整审阅文件与精确哈希

候选均位于 `05-06-contract-candidates/`。下表覆盖全部文件；`lifecycle.md` 的全文是规范细节，schema/proto 是精确字段布局，identity-profile 是身份规范；vectors 是测试输入，不是生产授权。manifest 自身不循环包含自身哈希，由本提案绑定。

| 文件 | SHA-256 | 发布目标 |
|---|---|---|
| [authorization.schema.json](05-06-contract-candidates/authorization.schema.json) | `3e5ee9e6fc0b8ccf517803ba5713dd1f3324e2dee41c0030f60657f046cd4f8b` | `contracts/authorization/v1/payload.schema.json` |
| [identity-profile.json](05-06-contract-candidates/identity-profile.json) | `aacabd62df0a6dadf8f02970e2fe2020d5dd5b889894f5df3e9613b9e4450070` | `contracts/protobuf/v1/identity-profile.json` |
| [lifecycle.md](05-06-contract-candidates/lifecycle.md) | `65874c4e00fa78159105e3a080e1898d923b67c0903556f99d10a516a09cd9c6` | `contracts/protobuf/v1/lifecycle.md` |
| [lifecycle.schema.json](05-06-contract-candidates/lifecycle.schema.json) | `30f613a44a8316e2ac8120d3f6f745499c7699329b7bbc7d30e17ef64eba230a` | `contracts/protobuf/v1/lifecycle.schema.json` |
| [runtime.schema.json](05-06-contract-candidates/runtime.schema.json) | `2e29ae94f438a2f79b16d7e755bb81875c8b8668dd5b52c15bde819cfe17469f` | `contracts/runtime/config.schema.json` |
| [vectors.json](05-06-contract-candidates/vectors.json) | `d62f617bfc6f5e3e90bae01a923c7ae01fa0ef26200db3adbc88f3da0bd721b7` | `review fixture only` |
| [vortex.proto](05-06-contract-candidates/vortex.proto) | `4a2458c511e9d5d9db0d567280445f6648d6482484ab19984a7c1a3289a05571` | `contracts/protobuf/v1/vortex.proto` |

Manifest SHA-256：`37627f6985aaa37df18c1f1f0734d7e4293a73066faa30cfa5b43c792943c346`。测试文件 SHA-256：`6a6798ccf4c955c3d3570b5d50722902a8e99a10e1b62a0a8240d8c7692870c9`。

## 各项增量及消费者

| 增量 | 精确字段/规则位置 | 后续消费者与验收 |
|---|---|---|
| 当前连接与区域状态 | LifecycleHello、HubStatus；session_id/region/Hub 身份匹配，旧流 fencing，blocked/online/autonomous/recovering | 05-07 实际节点切换；未知模式不能视为在线，超时不续权 |
| 上报与探测 | NodeReport、ProbeObservation；最多1024目标、16并发；仅引用已应用本地规则目标 | 05-08 实际探测/周期上报；UDP无有效健康响应报 unknown；遥测不提供可信心跳 |
| 远端续额 | RenewalRequest/Result；持久化请求 ID/哈希、原 delegation/source/boot/path/fence/snapshot，TTL1..300s，金额有界 | 05-08 实际低水位续申；同 ID 同载荷重放、冲突拒绝，换 Hub 无所有权/原结果不得再签 |
| 后端认证适配器 | HubControl.Connect、BackendHeartbeat/Allocation、CredentialRevocations、AuthorityRestrictions、EpochHandoffRequest、LedgerRecord/Acknowledgement | 05-08/12 真实 Hub 接入标记测试后端；有限原始委托；Phase6提供生产权威，不在本次伪造完成 |
| 身份统一 | identity-profile.json；同 node UUID 的 DNS+URI SAN、双 EKU、验证链/吊销、原子 TLS/HELLO signer 快照 | 05-09 实际控制/VLink 轮换；不用测试主机名或跳过证书验证；过期/吊销禁止新 OPEN/续期 |
| 恢复追平 | HubStatus 有限 recovery_target_sequence / confirmed_sequence / pending_tail_records | 05-12 FULL事务关闭旧 signer、封存S/转接R、进入recovering；追平有限屏障后在线，持续尾账单列 |
| 份额链及策略切换 | authorization 1.2.0 share_references、policy_transitions、record.share_reference_id/delegation_id | 05-13 完整父链/CAS/占用到真实Engine；新会话按保守时间切换，旧会话快照不变 |
| 运行配置 | lifecycle.region/hubs、调度参数、probe_targets、rule.share_reference_ids/policy_transition_ids | 05-07..13 配置消费；无地址扫描、无新资金来源、无数据库进入Engine |

## 关键安全语义

- 整体新消息上限1MiB，在分配前检查；整数不超过SQLite无损上限 `9223372036854775807`；哈希wire为32bytes，JSON投影为64小写hex。schema关键词与跨记录约束共同构成规范，schema通过不等于授权通过。
- 新生命周期 Protobuf 递归拒绝未知字段；JSON拒绝重复键、未知字段、空必填和多值。UTC RFC3339 Z时间；保留原硬截止，不用遥测或重连增加可信时间。
- 请求哈希为 domain `vortex.renewal.v1` + NUL + 清空自身hash后的确定性protobuf。响应必须绑定原请求和精确授权payload哈希。丢回执不换新请求ID重复扣/签；新boot不能使用旧boot权益。
- 后端授权与吊销只接受经过角色隔离mTLS认证的当前后端流；心跳包含会话/Hub/region绑定。节点自报及Hub状态不是源资金或时间权威。
- 撤销采用递增、累加的记录，不因分批遗漏而恢复旧身份。未初始化Hub明确blocked；不能因为连接成功就签发。仅临时/存储错误可标retryable。
- 份额根来自已认证持久化锚点；链深<=32，父revision、允许issuer、源/节点/规则/path及额度/时间均核对。Prepare与占用CAS同事务，未知激活保留新旧占用；消费/Commit都不能只检查叶子。
- 预发必须有已授权未来周期资金、租约与份额；配置候选提前24h、guard5s不产生新余额。可信时间跨边界不明确时拒绝新模糊会话；已有快照不变，未知负债留在原来源。
- 控制切换、超时、签发epoch变更、证书更换、进程重启和二进制回退均不能释放未知负债。Signer epoch与path generation保持独立。

## 能力范围与测试边界

| 能力 | Phase5承诺 | 留给后续/未完成 |
|---|---|---|
| 真实Agent/Hub/Engine与SQLite | 后续05-07..13实际进程 tracer、故障与不变量验收 | 本次仅候选可解析/可编译与反例，不宣称行为已实现 |
| 有限后端协议输入 | 认证心跳、有限委托、撤销、S/R/尾账ACK适配 | 生产CA/令牌/多租户资金分配/套餐策略是Phase6 |
| 安全升级 | 05-10/11服务排空、签名替换、兼容性与真实健康回退 | 回退不得恢复旧权威db/WAL或清除记账；不可逆迁移另立方案 |
| 验证 | 本机候选测试与现有冻结合同门禁 | 同一干净SHA Docker/Actions、新运行时UAT均待执行；Phase4性能资格不在此关闭 |

## 批准后的发布与回退步骤

1. 仅在所有者明确批准本文件精确SHA及以上版本后，将 manifest.targets 的候选原字节复制到正式契约目录。同步更新 versions、需求映射、CHANGELOG与原治理记录，保留历次审批。
2. control规范同伴 `lifecycle.md`、`lifecycle.schema.json`、`identity-profile.json` 的路径/哈希必须进入审批记录附属制品数组，并扩展现有冻结检查验证它们；不可只锁proto而让语义文件漂移。生成Go文件使用已锁protoc36.2和两个既定生成器。
3. 需求映射按AGT-001/002/005/007/012、HUB-004/013/014/016/018、SUP-B3-001以及新增消费者复核，不因字段存在就标需求完成。
4. 后续SQLite升级全部使用Agent/Hub各自的FULL加法迁移，重开/旧数据测试；现有schema/财务记录不破坏。启用新能力前检验读写兼容；回退只替换兼容二进制和明确非权威配置，不还原余额、ledger、closed epoch、fence或未知占用。
5. 完成05-06剩余任务和同SHA证据后再写完成SUMMARY；本检查点不代表计划或Phase5完成。若要求修改，重生成所有受影响哈希，重新审阅后才能冻结。

## 当前基线哈希（保持不变）

基线提交：`7fc7dba9e10a59b176a5091e0a63b5ed993a4c31`。候选测试按该可达提交读取旧契约进行兼容性比较；仓库需保留该提交供检查。当前工作区冻结文件另行检查无差异。

| 当前文件 | SHA-256 |
|---|---|
| `contracts/protobuf/v1/vortex.proto` | `5d1c2b2ef7d3b0d7163c8d09bb786b2ba4c528f3308cbf7e280064cafca5784e` |
| `contracts/authorization/v1/payload.schema.json` | `0febf4b5b7f23cf17f6121d22e7aa41071a3728b2953e462737844f2091c7ece` |
| `contracts/runtime/config.schema.json` | `43261cd1e9b115cddadee08b52ecabddf82f7e69c1f47690424568af91435ef8` |
| `contracts/versions.yaml` | `c1c4d18aa65cdd5a3825e38852872e165e4f4df571f78b7353177c471f9e6c8c` |
| `docs/governance/contract-review.yaml` | `5801336e52da6e52e84eb2fc82eb2208f416fc573b65749933058bf20b4a2c60` |

## 评审决定

待所有者选择：批准精确提案和三个候选版本，或提出修订。当前没有预填批准记录，也没有修改正式契约。具体测试和限制见 05-06-EVIDENCE.md。
