# Phase 4 剩余资格化核对（2026-11-05）

## Current evidence reconciliation — 2026-11-07

The full native dynamic matrix completed 27 combinations x 3 repeats on clean
`2543fb745d942ddf710c02d502a3675b9b3c7fe9`. Independent raw workload,
flow/source accounting, consumption formula and 65-second idle audits pass for
all 81 trials. Summary SHA256:
`135a289cdf96d46c4b3668befa45e87c289403c669541fe7fb5b1932abc4566a`.

Clean `0c2ba71c0653ca1c806dc3df1db93d847544011b` completed the separate
256 KiB / 30 s / 50% formal candidate: 300 s warmup, three 300 s trials,
independent TLS, 16 streams and actual 100 Mbps UDP. The gate passes; SHA256:
`5be4202a989dcb005011abe0f03e0184610519f2cf2b35c861829f7c6204baef`.
The 30% center candidate is now receiving a separate formal run; it must not
inherit the 50% result. Nine 600 ms renewal pressure trials failed availability
and remain raw failures. The 200 ms reservation deadline is unchanged.

The current comparative review is [04-44-D04-PARAMETER-REVIEW.md](04-44-D04-PARAMETER-REVIEW.md).
`locked=false` and `gaps_found` remain. Formal performance acceptance, D-04
parameter lock and persistent authority are retained as user-required debts;
production Agent/Hub/PostgreSQL authority remains owned by Phase 5/6.
Historical results below retain their original source and measurement scope.

## Current scope — 2026-11-06

当前关闭条件与归属以 [04-CLOSURE.md](04-CLOSURE.md) 为准。
Phase 4 实现范围已关闭，P4-Q1..Q5 资格缺口保留；持久化与生产后端
移交 Phase 5/6，不作为 M2 阻塞。本文件以下为历史证据，不重新激活越界任务。

## Prior result: Plan 04-12

## Current Q2/Q3 evidence — 2026-11-06

The final branch SHA `fdaea8f` now has a single auditable Q2/Q3 evidence
bundle. The Docker comparison and recovery runs cover the configured forged
OPEN, bypass, integrity, generation, drain, primary/active failure and spare
restart cases. Docker snapshot and finite TCP/UDP credit runs cover immutable
snapshot affinity, exact source conservation, exhaustion denial and no held or
unknown leakage. Targeted Engine race tests pass for identity, policy, share,
snapshot, grant, renewal, route and bounded UDP behavior. See
`04-39-Q2-Q3-COVERAGE-EVIDENCE.md` for output paths and scope.

These results close the finite current-configuration checklist only. They do
not establish exhaustive security coverage, production Agent/Hub persistence,
or full NFR-039/NFR-040/NFR-044 acceptance; those boundaries remain explicit.

See [04-12-EVIDENCE.md](04-12-EVIDENCE.md) for the authoritative current status:
21 D-04 static trials complete; 91e409e pool/mux latency gate passes; finite
runtime safety probes and native race pass. Production D-04 lock, complete
NFR-006 coverage, durable authority and final same-build formal performance
remain open. The older matrix and incremental notes below retain their
historical context and do not override this reconciliation.

The runtime bridge early-data slice is now covered on the current branch by an
authenticated delayed-authorization test. Before remote authorization, the
bounded TCP prefix remains unavailable to the remote stream; after authorization
the exact payload reaches the target and both accounting gates settle. UDP
continues to require OPEN/OPEN_OK. This updates the implementation boundary,
not the controlled-RTT or formal NFR-007 qualification status.

## Historical matrix

04-11 added a reproducible 27-case D-04 matrix and fail-closed qualification
gate. It records candidate coverage without promoting any parameter or
combining Docker, Actions and native results across SHAs.

审查基线 `6bfe976`；状态 **gaps_found / executing**。矩阵区分有限已验证行为、实现缺口及环境/测量缺口；不将参考规格变成新冻结基线。以下沿用路线图与当前 Plan 04-06 的目标。未重跑的历史证据保留原始 SHA，不视作本提交的新实测。

| 条目 | 已有证据 | 尚缺的实现或行为覆盖 | 尚缺的测量 / 环境 | 结论 |
| --- | --- | --- | --- | --- |
| NFR-003 三中继吞吐 | `baa0a02` 原生 16 streams / 60 秒，TCP 1086–1200 Mbps；同负载独立 TLS 比例 82.64%，均超过当前 400 Mbps / 70% 目标；cba1bef 另有显式模式短诊断 | source 真实授权 Gate、D-04 续申及完整门控仍未接线，不能声称正式门控性能 | 未提供各转发节点独享资源和独立链路资格证据；缺预热 60 秒、正式 300 秒 × 3 的同条件 runtime/TLS 组 | 诊断目标已达到；正式资格未通过 |
| NFR-005 UDP 公平性 | `baa0a02` 原生三次实际约 100 Mbps、1,200 bytes、TCP 满载，丢包 0.048004% / 0.010241% / 0.004480%，丢弃分类和计量相符 | 仍需在真实授权门控及耗尽/续申压力下验证 TCP/UDP 行为 | Actions scale 实际 24.21 Mbps；资格化不能以 requested rate 替代 actual rate；正式运行需原始发送/接收/重复/乱序和各层丢弃证据 | 有限混合负载通过；正式资格未通过 |
| NFR-006 完整性/顺序/许可 | 64 MiB 确定性文件 SHA-256、一条配置路径的多跳 OPEN、六类伪造请求、两个旁路端点；d0c1cb2 八组真实进程切换/恢复三宿主通过 | 攻击矩阵仍为有限集；缺针对当前配置的系统化绕过/身份/修订/路径组合覆盖及其完整清单，不能声称“所有伪造请求”已证明；恢复不等于权威账本恢复 | 与当前最终授权配置在同一 SHA 复核完整性/故障场景；该功能项不应只因没有独享硬件而搁置 | 有限行为通过；覆盖未完整 |
| NFR-007 pool/mux 首字节（跨 Phase 3） | cba1bef 分模式、50 个原始样本，原生 pool 1.0703 ms / mux 1.3188 ms；当前分支另有延迟授权 early-DATA runtime 回归；链路复用已证实 | 有界 TCP early DATA 在授权前保留于 OPEN 缓冲，授权后精确交付；UDP 仍必须 OPEN_OK 后发送。仍不能以低 RTT 数值证明无额外往返 | 当前各段 RTT 是独立 TLS 用户态 echo 诊断，不足以验证 VLink 额外等待；需受控逐段 RTT 扫描、时间线或抓包、复用/新建链路分别记录；pool/mux 分开，计费等待例外单列 | 实现切片通过；正式测量未通过 |
| NFR-039 政策 T / 保护带 | c8c2a37 has finite immutable snapshots. Worktree policy adapter tests conservative interval, exact-T switch, <=10s pause/recovery and hard cutoff under injected clock | Not wired into relay; trusted clock-health input, dynamic pre-issue provider, 200ms one-shot issuer request and real policy/runtime/share lease path remain absent | No runtime clock source or process topology evidence for the adapter; full q=T−60 / ttl=300 and <=12s evidence must be run after safe wiring. Finite unit cases do not qualify NFR-039 | Partial Engine unit behavior only; full requirement not passed |
| NFR-040 share CAS / 排空 | `d7ca856` share Manager + injectable runtime ShareGate tests prepared/active coexistence, 50→60→40 occupancy max, old lease drain, fencing/idempotency and boot rejection; Docker check passes | ShareGate is not wired to relay config; durable Agent/Hub CAS, delayed/lost receipt reconciliation, renewal protocol and rate-share enforcement remain absent. Path generation drain is not share CAS | Native runtime / Actions aggregate terminal result remains pending; no production topology qualification | Finite injected Engine adapter passes; full NFR not passed |
| NFR-044 快照与路径系数 | c8c2a37 runtime `CreditBook` 在原生/Docker/Actions 验证 1→2 快照选择、固定旧会话、不同系数、迟到授权隔离、预留资金和零泄漏 | 仍缺完整 Event/Flow 来源绑定、跨路径 1→3 守恒、跨政策 T、grant/batch/final 篡改及完整按来源守恒闭环。当前 UDP 切路终止是用户已选策略，按该策略验证旧 UDP 未结算负债，不新增“必须保留旧 UDP”要求 | 持久化权威账本是后续阶段，不要求 Engine 连数据库 | 有限 runtime slice 通过；完整 NFR 未通过 |
| D-04 参数评审 | 本轮逐项审查见 04-06-D04-REVIEW.md；VLink 窗口调优、计量和混合负载结果已有 | 缺真正可扫的授权块/水位/续申/闲置实现及执行点接线 | 缺候选参数对照、资金占用/申请等待等原始观测及最终评审确认 | 评审完成；候选未锁定 |

## 环境核对

- Docker：macOS + OrbStack Linux VM，适合功能/集成回归；不具备各转发节点独享 4 vCPU / 8 GiB 和独立实链路证据。
- 已授权 Ubuntu：24.04.5 amd64、KVM、约 4 vCPU / 8 GiB；source、三中继、destination、driver 共用一个 host / loopback，不能等同于每节点独享配额。虚拟化本身不是失败依据，缺的是资源隔离、链路条件与实测证明。
- Actions：ubuntu-24.04 托管机，同 SHA 结果用于回归；共享 runner 不是独享正式压测拓扑。41813036017 的低实际 UDP 速率仍需修正/选用满足发生率的负载端，零丢包不掩盖 offered-load 不足。
- 同 SHA 回归：本轮在干净 6bfe976 上执行本机 phase4-m2-check 通过；Actions [41838307562](https://github.com/VortexWang/Vortex/actions/runs/41838307562) 已查询确认为 success。其前一恢复代码 run 为 41836183498 / d0c1cb2。没有把不同 SHA 的性能结果合并成新资格化。
- 24 小时稳定性 NFR-008..011 属于后续整体稳定性/资格化工作，不能作为 NFR-003 已完成或未完成的替代判据；其未完成应单独记录。
- Phase 3 的 NFR-002/004/007 保持原验证边界：候选数字和有限测试已记录，生产路径资格并未通过；它们不能由 Phase 4 三中继吞吐间接覆盖。

## 可执行的补齐顺序

1. **实现缺口先行**：有限余额/隔离快照的测试 issuer、source 授权 Gate 接线、D-04 块/续申/耗尽/未知状态。保持 Engine 无数据库、无主动控制面调用；用注入接口验证，不把持久化前移进 Engine。
2. **同一授权链路补功能证据**：NFR-039 时钟切换、NFR-040 份额 CAS/排空、NFR-044 快照和逐来源守恒；NFR-006 攻击/故障清单；这些先在 Docker/CI 完成，不等待独享机器。
3. **NFR-007 专项**：早期 TCP DATA 的有界、安全转发方案及真实运行时实现；建立受控 RTT 的 pool/mux 对照和包/事件时间线。无授权消费必须始终为零，不能为了延迟绕过许可检查。
4. **D-04 扫描并评审**：采用评审文档的实验矩阵，提交具体结果和参数确认表；未实测前不冻结候选。
5. **正式资格化**：满足资源/链路条件后，在同一干净 SHA 做 16 streams、60 秒预热、300 秒 × 3 与独立 TLS，正式授权门控开启；本机 Docker / Actions 同 SHA 回归分别留证。性能目标差距如实记录，继续用户已授权的正确性工作，Phase 4 不虚标完成。

只把可实现、可验证的工作纳入下一执行切片。原始统计、命令、exact config hash 与 semantic manifest hash 各自留存；跨环境地址和证书不同，不要求 exact config hash 相同。不会要求找回重启前的十一份旧材料。


## Source credit 增量 — 6a1faf2

新增 optional source_credit：静态单快照 TCP 有限预算/本地分配和续补
已在同干净提交 Docker/原生验证；标准性能 workload 尚未开启它。
因此矩阵中“source Gate 缺失”现更新为“仅静态 TCP 有限 authority
已接线，完整正式授权和混合负载尚缺”。NFR-003/005 性能、NFR-039/
040/044 行保持未通过。尚不允许 UDP listener 或热 route set 与此
实验 Gate 混用。详情见 04-06-CREDIT-EVIDENCE.md。


## d2f21f8 增量

静态单快照 source Gate 现支持 UDP 双向预扣及 TCP/UDP 交错共用预算；
可信进程内签发请求/结果已加入身份绑定、幂等、pending/unknown 占用
和迟到确认测试。热切路/多快照/系数、远端 Hub、200 ms 请求、动态
授权目标与正式性能门控仍缺。故原有“不支持 UDP / 没有任何 grant
ID”的描述仅适用于此前切片；不扩大 NFR/D-04 验收结论。详见
04-06-GRANT-UDP-EVIDENCE.md。Phase 4 仍 executing。
