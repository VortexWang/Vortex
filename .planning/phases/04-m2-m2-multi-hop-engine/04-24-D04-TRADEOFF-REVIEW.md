# D-04 M2 参数取舍复核（2026-11-06）

状态：候选评审补充，未锁定。按 04-CLOSURE 的 P4-Q4 范围执行，
不要求生产 Agent/Hub 持久化先完成。

## 数据校验和口径

复读本地原始 `artifacts/phase4/d04-scan-buffered-d46ca8e/summary.json`
及全部 21 个 `events-source.jsonl.credit.json`，summary SHA256 与
04-18 登记一致：b33db5f6fc0eabd8c741899b27db49f64f8a5cfe41e101eb16200217ceac5e87。
测量 SHA d46ca8e793a9b7f79c5e65ca011401bdbd76bdde；不是本次文档提交的新性能结果。

每个候选三次试验；TCP 为 mixed 吞吐中位数；申请/GiB 为每次 grants /
(used_bytes / 2^30) 后取中位数，覆盖整个试验而非仅 mixed 阶段。
p99 为三次 issuer 直方图 p99 桶上界最大值，不能当作精确百分位或网络 RTT。
held 为峰值最大值；available 为试验结束时可用额度中位数，不能当作峰值、
平均占用或泄漏。每块 reservation 数是分配摊销指标，不是异步请求合并率。

| 候选 | mixed TCP Mbps | 申请/GiB | issuer p99 上界 ms | 峰值 held MiB | 结束 available MiB | reservation/块 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 256 KiB / 30s / 30% | 1026.43 | 4.14 | 0.262143 | 125.70 | 261.36 | 237.90 |
| 初始块 64 KiB | 1022.59 | 4.14 | 0.262143 | 127.01 | 272.61 | 225.93 |
| 初始块 1 MiB | 1025.54 | 4.13 | 0.131071 | 129.20 | 252.02 | 249.90 |
| 目标 10s | 1025.00 | 12.28 | 0.131071 | 125.76 | 102.94 | 237.69 |
| 目标 60s | 1026.78 | 2.13 | 0.032767 | 127.36 | 619.99 | 237.87 |
| 水位 20% | 1024.50 | 3.67 | 0.131071 | 126.43 | 339.61 | 237.66 |
| 水位 50% | 1022.47 | 5.80 | 0.262143 | 129.03 | 348.73 | 237.41 |

所有块实验使用相同 4 MiB 上限，测试的是初始块，不是固定块大小。
所有目标秒数从固定 100 Mbps 换算；不是最近一分钟实际消耗驱动的目标。

## 评审结论

保留 256 KiB / 30s / 30% 作为下一轮中心候选，不修改默认值。
这是一项权衡选择，未声称统计最优：

- 64 KiB 未在本组降低峰值 held；1 MiB 的每块摊销更高，但峰值 held 更高，
  没有充分证据表明值得提高冷启动块。
- 10s 相比 30s 约三倍申请密度，结束可用额度较少；60s 申请更少，
  结束可用额度更多。需用时序占用与突发/闲置实测衡量代价。
- 50% 水位比 30% 申请更多，本组没有相应吞吐收益。同步 issuer 几乎没有
  申请延迟压力，不能据此证明 20% 水位在延迟/丢失响应时仍安全且足够。

## 阶段内剩余锁定证据

1. 在可注入、有限余额的 M2 issuer 上验证动态消耗目标与启动 floor；
   固定 100 Mbps 对照仍保留为可复现基线。
2. 对 20/30/50% 水位加入确定性延迟、拒绝和未知响应，核验 200ms 等待、
   在途合并、不重复签发/消费以及 unknown 上界；不需要真实生产 Hub。
3. 用时序采样区分 available、held、pending、unknown；覆盖空闲收缩与
   只有安全未用证明才能释放的行为。生产 grant-return 协议仍是独立审批项。
4. 将明确的阶段内参数评审与正式 60s/3×300s/TLS 资格化各自留证；本组
   15s 诊断不能代替最终资格验收。后续生产条件复验归 Phase 5/6。

## 评审数据入口加固

`percentile_bucket` 现在拒绝负数、布尔、非整数计数、超过 64 桶的直方图
及越界/非有限百分位。防止畸形数据被计算成可用于评审的等待指标。
20 项 Python 门禁测试通过；已有 21 个结果的 63 个 p50/p95/p99 值逐条复算，与原结果一致。

## 后续 40ms 延迟专项

同一干净 5239ba5 上，专用 Ubuntu 的 20/30/50% 水位各一次 15s×2
五进程试验均正确性通过：mixed TCP 999.50/1000.05/1001.49 Mbps，
UDP 约 100Mbps 且零丢包。申请 30/35/49 次，每组一次等待成功、无最终
拒绝。见 04-26-NATIVE-DELAY-WATERLINES.json。这是可控延迟下的新证据，
不能与 d46ca8e 当作同 SHA 对照；单次结果不锁定水位。40ms 未对稳态
续申造成明显压力，继续提高延迟/失败压力而非重复无差异扫描。

## 600ms 续申压力：候选仍未锁定

5bfb9b2 将首次有限供额与续申延迟分开。在相同固定目标和负载下，
20%/30% 水位各出现 16 次等待超时，TCP 基线约 1.40s 即中断；30% 的
混合 UDP 丢包 6.676441%。50% 单次正确性通过，88 次等待均成功，
mixed TCP 1009.277Mbps、UDP 99.9648Mbps、零丢包。三组逐来源额度均
守恒，结束无在途或未知额度，不能据此将失败组的传输认定为正确。

见 04-27-NATIVE-RENEWAL-DELAY.json。此前 30% 中心候选不能直接锁定：
在 600ms 延迟条件下需要重新评估续申提前量。50% 仅获得本条件下的
单次支持，尚缺重复、动态目标及占用证据。保持 200ms 等待上限，
不调整默认值，也不将这些 15s 短测冒充正式性能资格。

## 动态目标原生接线验证（172b0a7）

专用 Ubuntu 五进程，30s 动态目标、8MiB floor、375000000-byte ceiling，
首次供额不延迟、续申 40ms，16 TCP、每阶段 15s、100Mbps UDP。
20/30/50% 水位均 exit 0、runtime_correctness pass，逐来源守恒并结清。
mixed TCP 分别 994.932/987.820/994.448Mbps，UDP 99.968/99.968/99.96416Mbps，
全部零丢包；155/143/166 次等待均成功，无 timeout/cancel/final refusal。
申请 44/49/66 次，最终目标均达到上限；sessions/held/pending/unknown 均零。

完整报告、credit 和 semantic manifest 及 SHA256 见
04-30-NATIVE-DYNAMIC-TARGET.json。脚本独立脱离 SSH 运行，结束后确认
rmem_max/wmem_max 恢复 212992。此组是动态目标实际接线证据，但没有
时间序列证明目标何时触顶，也没有证明长时间闲置或各目标秒数的性能。
每水位仅一次，不能以吞吐相近锁定参数。D-04 保持 pending-review。

同一干净 172b0a7 本地 Engine 全包 race 通过；17 项真实进程 OPEN/断链
检查通过（04-31-CURRENT-ATTACK-EVIDENCE.json）。有限覆盖登记见
04-29-CURRENT-COVERAGE.md；最终独立 TLS、Docker/Actions 和原生完整
资格仍分别待验证，不混用本地与原生结果成为一个 pass。

## 有界占用时序接线（94737c3）

Observe 开启时，每秒采样 CreditState，最多 3600 份周期记录，另外保留
关闭后的最终状态；超限显式记录 dropped_periodic_samples。不在默认热路径
开启采样，不以离散样本最大值冒充真实瞬时峰值。

专用 Ubuntu 的 30s 动态目标/30% 水位/40ms 续申实验通过正确性：
16 TCP、15s 双阶段；mixed TCP 993.679Mbps，UDP 99.97056Mbps、零丢包。
37 份采样覆盖 35.965s，无采样上限丢弃，每份均满足逐来源守恒；从
8MiB 初始目标增长至 375000000-byte 上限。175 次等待全部成功，50 次
授权，最终 sessions/held/pending/unknown 均零，双向计量总和
8887885032 bytes 与 source used 一致。原始时序/报告/配置及校验和见
04-32-NATIVE-CREDIT-TIMELINE.json。sysctl 两个 max 已恢复 212992。

此轮未覆盖 60s 以上闲置，不能证明实际进程的闲置收缩曲线；故障导致的
unknown 时序也尚未采得。延迟 issuer 的旧 issuer_nanoseconds 直方图
仍为零，不能用它声称实际续申延迟分位数；Waits 仅证明结果计数。
命令与 accounting race 通过，采样器并发停止/保留最终样本回归通过。

## 原生 65s 闲置窗口（1ffc1f0）

相同动态目标中心候选，15s 双阶段负载后保持五进程运行并闲置 65s。
干净 1ffc1f0 正确性通过；mixed TCP 996.641Mbps、UDP 99.95776Mbps、
零丢包，175 次等待全部成功。103 份状态覆盖 101.037s，无上限丢弃，
逐份核验非负、时间顺序及额度守恒。目标曾达 375000000 bytes，第
93.001s 的采样回到 8MiB；第 94.001s 已无会话/held/pending/unknown，
之后余额与授权次数不变。最终 used 8907366760、available 238709440。

该实验实证“目标回落、闲置尾块安全回到本地池”，不声称已签发可用
额度会退回 issuer：目标缩小没有将 available 变成 unissued。生产
grant-return 仍属 Phase 5 协议事项，不能因此引入 Phase 4 新关闭前提。
本组没有非零 unknown，故不能替代未知响应故障证据。

04-33-NATIVE-IDLE-TARGET.json 保留原始 SHA256、报告、语义配置、闲置
窗口和逐秒状态投影；投影省略 allocator 直方图，原始时序仍在测试机。
两个 sysctl max 恢复 212992 并实查。20 项 Python 门禁测试通过，非法
闲置时长在启动进程前被拒绝。正式资格及 D-04 参数锁定仍未完成。

## 原生未知续申故障（f9d99ec）

仅资格测试的显式 outcome 注入：首次有限额度接受，之后续申在 40ms
返回 unknown。既有生产和默认测试路径不变；未知响应不能视作拒绝或
未签发证明。单测验证 16 次后续尝试和关闭均不替换/释放 unknown；
accounting/command race 通过（2.381s / 1.983s）。

专用 Ubuntu 五进程、30s 动态目标/30% 水位/8MiB floor：负载试验
exit 1，TCP 中断，混合 TCP 为零、UDP 100% 丢失，没有通过报告。
launcher exit 0 仅表示脚本留证完成。22 份采样全部逐来源守恒；第
4.000s 已观察到 unknown_grant=6029360，之后直至关闭均保持冻结，
grant_records=2、accepted grants=1 不变。最终 used=8381768、available=6840，
sessions/held/pending 均零；unknown_grant 非零，因此不得称“全部结清”。

04-34-NATIVE-UNKNOWN-RENEWAL.json 保存原始 hash、状态投影、失败负载
及语义配置。此项证明故障下不重复签发、不无资金释放；不能证明正常
吞吐，也未证明真实丢失响应后恢复成功。测试机两个 sysctl max 均已
恢复 212992。明确拒绝、迟到恢复和重复参数对照仍待原生验证。

## 明确拒绝续申的适配器证据（further runtime slice）

新增 `TestFaultedRenewalRejectedIsDefinitiveAndConserved`，用 40ms 延迟的
进程内 issuer 返回明确 `rejected`。测试先耗尽首个 accepted grant，再验证
续申失败为 `ErrDenied`；pending 归还 `unissued`，`unknown_grant=0`，不生成
替代 accepted grant，wait 统计记录 final failure 且不计为 timeout。冷启动
本身的一次等待作为基线保留，续申断言按增量计算。

该证据补齐了拒绝结果与 unknown 结果的语义分界，仍不代表远端签发、丢失
回执恢复或持久化权威。正式性能资格、D-04 锁定和 Agent/Hub 持久化债务
保持未完成。详见 `04-35-REJECTED-RENEWAL-EVIDENCE.md`。
