# D-04 参数评审（2026-11-04）

## Current stage boundary — 2026-11-06 owner decision

M2 参数关闭按 [04-CLOSURE.md](04-CLOSURE.md) 的 P4-Q4 执行：允许有限、
可审计的进程内 issuer，必须标明指标适用边界。下方 durable 实现和其
尚缺验收移交 Phase 5，不再作为 M2 参数评审前提。生产参数需在实际
Agent/Hub 集成时复验；本次未锁定任何候选值。

## Historical cross-phase reconciliation — 2026-11-06

Status: **not locked; durable implementation integrated, full qualification open**.
The historical rows below describe earlier slices and must not be read as the
current implementation inventory.

| Requirement | Current implementation/evidence | Remaining acceptance |
| --- | --- | --- |
| Immutable authority isolation | DemandScope, exact scoped ledger read, Agent/Hub SQLite and TLS issuer binding | Full cross-tenant/multi-hop hostile matrix and product startup wiring |
| Cold start / dynamic target / renewal waterline | Explicit candidate parameters; 60-slot durable committed-consumption ring; scoped valid balance; opt-in startup wiring; one actual metered TCP and UDP control 1.3.0 demand→durable issue→install→same-stream renewal cycle | Sustained durable-issuer workload, controlled candidate scan; no defaults locked |
| Coalescing / delayed responses | Persisted UUID, atomic success/refusal, replay after restart, source conservation and refusal backoff tests | Delayed/lost response injection during sustained native TCP/UDP workload |
| Missing-authority wait | One 200 ms context covers initial attempt and retries; 128-waiter bound; scope-bound control wakeups | End-to-end native load tail latency, including cancellation and queue pressure |
| Native correctness | Clean 382aab9: ten SQLite contention repetitions, Agent/Hub/Engine race and vet PASS on dedicated Ubuntu | Does not establish performance of the durable allocator |
| Session block strategy | Earlier process-local block candidate sweep exists; durable gate reserves per write | Implement/measure bounded durable session-block strategy or explicitly review a different strategy; do not claim the old scan covers it |
| Idle shrink/reclaim | No complete grant-return proof path | Persist idle lifecycle, stop/fence grant use, settle all holds, transfer exact unused proof, deduplicate issuer credit and preserve unknown liability |
| Parameter decision | 256 KiB -> 4 MiB, 8 MiB floor, 30 s target, 30% waterline remain candidates | Review measured throughput, latency and tied-up funding together before locking |

The c6a0761 formal performance pass remains valid only for that old implementation.
It must not be relabelled as performance of 382aab9's durable settlement path.
See 04-14-NATIVE-REGRESSION.json for the newer finite native regression hashes.

### Idle proof boundary

Hub HandoffHub transfers only unissued delegation remainder. It does not return
Agent-held credits. Control 1.3.0 CreditDemandDenied proves only that one request
issued no rights; it cannot be used as an unused-credit receipt. Expiry, closed
transport, usage ACK and a local zero-session observation are individually
insufficient proof for reclaim. A proper return path must fence further use,
retain unresolved reservations/unknown upper bounds, reconcile the exact grant,
and atomically record a one-time return at the issuer before reallocation.
This is a substantive remaining implementation requirement, not a documentation
exception or something covered by the native green test suite.

## 04-12 measured review

The candidate inventory has now been executed as seven one-factor cases x
three repetitions on d1ee7d2. All 21 source ledgers reconcile; all UDP runs have
zero loss. TCP medians span 1030.47–1034.22 Mbps and short-flow medians span
1.665–1.694 ms. This small spread does not establish a winner. Keep the proposed
256 KiB initial block / 4 MiB ceiling / 30-second adaptive target / 30% waterline
as **unlocked** candidates. The sweep used a fixed 100 Mbps grant-rate reference,
not a production adaptive issuer. Delayed/lost issuance, drain/idle policy and
durable funding observations still need integration evidence. Full table and
raw evidence identifiers: [04-12-EVIDENCE.md](04-12-EVIDENCE.md).

The prior reviews below are historical.

状态：评审已完成；**候选保留，未批准锁定**。代码审查基线 `6bfe97624cf807824d8df91fa7519b5a108e2d04`，与 `d0c1cb2` 相比仅阶段文档改变。本文是当前项目评审，不将 V1.11 升格为冻结验收依据，不增加新的历史材料门禁。

## 04-11 gate update — 2026-11-05

`tools/phase4/d04_scan.py` now emits the full 27-case one-factor matrix
(64/256/1024 KiB x 10/30/60 s x 20/30/50%) with an 8 MiB floor. Generation
does not lock any value. `tools/phase4/qualification_gate.py` rejects missing
runtime authority, isolated native resources, dedicated link proof, less than
60 s warmup or fewer than three trials. No eligible host and no runtime-
authority D-04 workload were available in this run, so the candidates remain
unlocked and the formal gate remains not-qualified.

## 结论

D-04 是 ENG-037/ENG-050 的授权和预扣策略，不是 VLink 的流控窗口。现有真实三中继压测证明了 tracer 的有界队列、写入计量及有限故障恢复；没有比较过不同授权块/水位，也没有真实续申链路，因此不能从这些结果推导“D-04 最优”或完成冻结。建议保留下面的候选值作为下一轮实验的中心点，不修改产品默认值、不声称用户已批准。

## 参数逐项审查

| 参数 | 当前参考候选 | 当前实现和证据 | 本轮决定 |
| --- | --- | --- | --- |
| 每方向预扣块 | 256 KiB 起，用满翻倍，上限 4 MiB | `accounting.Batch` 实现一次性结算/未知结果；runtime bridge 按帧长度 Reserve，没有自适应块分配器 | 保留候选；先实现会话双方向块管理、短写返还及未知上界冻结，再扫块大小 |
| 授权隔离键 | rule / execution point / immutable snapshot | `Snapshot.ValidateGrant` 只检查 ID/系数；memory Gate 仅有 flow key，没有授权账本及隔离余额 | 必须补齐隔离，不允许用跨快照余额提高吞吐 |
| 冷启动授权 | 每键 8 MiB 原始字节，受安全可签发余额限制 | memory Gate 无余额上限、资金来源或 issuer 响应 | 保留候选；比较短流占用和并发启动申请量 |
| 后续授权目标 | max(8 MiB, 最近一分钟速率 × 30 秒) | 未实现一分钟速率估计或基于消耗的续申；既有吞吐报告未观测该机制 | 保留为中心候选；必须同时报告资金占用，不能仅按吞吐选值 |
| 续申水位 | 剩余 30% | 未实现阈值触发、在途请求合并、延迟/失败/未知响应处理 | 保留候选；注入延迟和失败实测，禁止重复请求重复增加权利 |
| 无授权等待 | 最多 200 ms，失败拒绝 | 当前 source TCP bridge 传入 nil gate；destination 使用内存计量 Gate，不执行真实签发等待 | 补齐执行点接线及有界申请；无额度不得先写后补 |
| 无会话收缩 | 10 分钟缩至 1 MiB；24 小时回收 | 没有相应计时、最终未用证明和回收实现 | 保留候选；可注入时钟验证边界，不能把到期当释放证据 |
| 未知负债 | 保留预扣上界直至有安全证明 | Batch 和短写注入有有限证据；memory adapter 无权威余额，进程退出不保存未知账本 | 在进程内实验 issuer 上先验证余额冻结和重试；生产持久化由 Agent/Hub 阶段承担 |

`runtime/accounting/memory.go` 的事件容量在结算时检查，Reserve 并不扣减有资金支持的余额。事件条数上限不是授权上限；它不能用来测授权耗尽。当前 destination 计量证据也不能替代 source 执行点的授权门控。

## 已测参数与 D-04 分开记录

当前 `runtime/link/supervisor.go` 的候选：stream window 2 MiB、aggregate window 8 MiB、MaxWindow 16 MiB、UDP reserve 2 MiB、TCP receive 256 frames、UDP receive 2,048 frames / 2 MiB；source UDP 每流 256 packets / 256 KiB、最多 1,024 flows。adapter 默认最多 1,000,000 events，资格化显式配置最多 10,000,000；4,096 条分块只控制事件存储开销。这些参数已经改善混合负载，但均不是授权块、续申目标或资金预算。

- `baa0a02`：原生 16 TCP streams，60 秒/阶段，三次实际约 100 Mbps UDP，丢包 0.048004% / 0.010241% / 0.004480%，TCP 1086.41 / 1169.10 / 1200.00 Mbps；写入计量对账一致。
- 同 SHA、同负载独立 TLS：runtime 1086.41 / TLS 1314.60 Mbps = 82.64%。它与 TCP-only contention retention 是不同指标。
- 同 SHA 本机 Docker 和 Actions 41813036017 的 scale regression 通过；Actions 实际 UDP 仅 24.21 Mbps，不能算 100 Mbps 验收。
- `cba1bef`：Docker / 原生 pool、mux、auto 分别有 5 秒诊断与首字节原始样本；Actions 41830048180 通过。不得与 `baa0a02` 拼成同 SHA 的一次资格化。
- `d0c1cb2`：Docker / 原生 / Actions 41836183498 八组授权路径及恢复场景通过；未提供 D-04 续申实测。

详细记录见 04-06-NATIVE-SCALE-EVIDENCE.md、04-06-NATIVE-TLS-EVIDENCE.md、04-06-AUTO-EVIDENCE.md、04-06-RECOVERY-EVIDENCE.md。

## 占用量级与实验设计

以下只是公式分析，不是实测。单个隔离键持续速率若为 100 Mbps，30 秒目标为 375,000,000 bytes（357.6 MiB）；400 Mbps 为 1.5 GB（约 1.40 GiB）；1 Gbps 为 3.75 GB（约 3.49 GiB）。在稳定速率下，30% 水位约提供 9 秒续申提前量。速率突增、未知签发、资金不足、计费系数和来源分配会改变可用量；这些数值不是一次性内存分配，也不是可无资金签发的承诺。

下一轮实验按以下顺序执行，避免未接线就测“参数”：

1. 通过 `accounting.Gate/Session` 注入有限余额的进程内测试 issuer，实现 rule/execution/snapshot 隔离、每方向预扣块、续申、截止、未知冻结；Engine 不访问数据库或主动调用控制面。issuer 是测试边界，不能称为持久化账本。中继/出口计量与 source 授权分开。
2. 在真实 runtime 接入上述 Gate。先验证零额度无目标写入、耗尽停止、部分写入、重复/迟到授权、未知响应不再签、快照切换、闲置证明回收，以及 200 ms 边界。安全失败必须使检查退出非零。
3. 在一个干净 SHA 上做基线及单因素扫描：初始块 64/256/1024 KiB（最大 4 MiB）；授权目标 10/30/60 秒（最低 8 MiB）；水位 20/30/50%。这些是实验值，不是新增批准值。先固定其他参数逐组扫描，再测入选组合，避免将三组独立最优值直接拼接。
4. 每组覆盖短流、16 条长流、TCP + 1,200-byte / 实际 100 Mbps UDP、资金不足和授权延迟/丢回执。记录实际 offered rate、吞吐/丢包、首字节、申请频率/合并数、等待 p50/p95/p99、余额/在途/未知占用峰值、按键守恒、CPU/RSS。测试时钟用于闲置/截止语义，不能当作真实长稳运行。
5. 同 SHA 本机 Docker 与 Actions 使用相同入口及语义 manifest；原生合格拓扑另记录资格化条件。前后配置、原始结果和失败均留存。安全守恒先通过，再权衡吞吐、尾延迟、签发开销与资金占用；完成实测后给项目所有者具体参数表确认锁定。

## 本轮处置

无需再次选择“是否继续”。下一项是 Plan 04-06 内的有限授权 adapter + source 接线及上述安全场景，之后才能做 D-04 扫描。当前仍为 Phase 4 executing；既有原始结果保持原始 SHA 和环境标识。与硬件无关的实现缺口应先补齐；不要求回收项目重启时已删除的文档。


## 实现进展补充 — 6a1faf2

首个 source_credit 切片已提供启动固定快照的 TCP 有限余额 Gate、
每方向预扣块和同步本地续补，并通过 Docker/原生的精确耗尽测试。
前文缺口表是 6bfe976 审查基线；本补充更新其中“完全没有 source
Gate/块管理”的状态。当前标准混合负载脚本仍未开启该 Gate。
远端 grant identity、未知签发响应、UDP/热切路/跨快照、动态授权目标
和闲置策略尚缺，不能据此冻结 D-04。见 04-06-CREDIT-EVIDENCE.md。


## d2f21f8 增量

静态单快照 source Gate 现支持 UDP 双向预扣及 TCP/UDP 交错共用预算；
可信进程内签发请求/结果已加入身份绑定、幂等、pending/unknown 占用
和迟到确认测试。热切路/多快照/系数、远端 Hub、200 ms 请求、动态
授权目标与正式性能门控仍缺。故原有“不支持 UDP / 没有任何 grant
ID”的描述仅适用于此前切片；不扩大 NFR/D-04 验收结论。详见
04-06-GRANT-UDP-EVIDENCE.md。Phase 4 仍 executing。

## Durable per-write accounting baseline — 2026-11-06

A clean native run of the real Agent Store reserve/settle path at commit
`d46ca8e793a9b7f79c5e65ca011401bdbd76bdde` measured approximately 6 ms per
operation across three payload sizes, with median accounting capacity rising
from 0.18 MB/s at 1,200 bytes to 10.14 MB/s at 65,536 bytes. SQLite WAL/FULL,
source conservation and pending outbox cardinality were checked for every
sample. Full command, five-sample ranges, host details and hashes are in
`04-17-NATIVE-ACCOUNTING.json` and `artifacts/phase4/durable-accounting-d46ca8e/`.

This isolates durable per-write cost only. It does not compare authorization
block policies, include transport or Hub issuance, or establish user-visible
throughput. Keep the existing D-04 candidates unlocked until the planned
five-process authorized TCP/UDP scan compares durable bounded blocks and
per-write accounting while recording latency and held-funding costs.

## Seven-case native diagnostic comparison — 2026-11-06

`04-18-NATIVE-D04-SCAN.json` adds 21 successful 15-second trials at clean
`d46ca8e`. All seven one-factor cases have zero UDP loss and conserved/settled
source credit; mixed TCP medians span only 1022.47–1026.78 Mbps. This narrow
spread gives no basis to declare a winning production configuration. Each
trial's formal performance status remains not-qualified.

The scan covers actual five-process forwarding with the process-local finite
allocator. It does not measure the persistent Hub scheduler or a durable
session-block strategy, and fixed-reference target-size conversion is distinct
from dynamic consumption-based renewal. Keep all D-04 defaults unlocked.
Sustained durable-authority traffic, lost/delayed control responses under load,
funding occupancy, idle-return proof and formal same-SHA qualification remain
required. The unbuffered failed baseline remains visible in the diagnostic
archive; temporary buffer maxima were restored after the successful scan.

## Native durable renewal update — 23a9ea3

Three clean native race trials now each complete 32 TCP and 32 UDP adaptive
renewals with real mTLS, durable Agent/Hub issuance and a protocol-backend usage
fixture. Snapshot audits verify all 68 batches per transport and exact source
conservation, with no pending requests or open sessions. Full Agent/Hub native
race and vet also pass; see 04-19-NATIVE-LONG-RENEWAL.json.

This establishes the controlled multi-cycle integration slice after reducing
exhausted-grant planning queries. It does not qualify throughput/latency at
production load or implement a durable block allocator. Keep parameter lock,
complete hostile coverage, sustained mixed load and idle return open.
