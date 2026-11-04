# Source TCP 有限额度执行切片 — 2026-11-04

代码提交：`6a1faf26abd5d37100ea56ede455a38092e8c6c2`（功能提交 a048cd4 + readiness 修复）。状态：本机 Docker、原生 Ubuntu 与 GitHub Actions 均通过。Actions [41849553357](https://github.com/VortexWang/Vortex/actions/runs/41849553357) 在干净 SHA `6a1faf2` 全步骤成功，包括全量 race、有限额度、模式、混合负载和恢复。Phase 4 仍 executing；本记录不批准 D-04 参数、不授予正式性能资格。

## 实现和审查范围

- 新增 `runtime/accounting.CreditGate`，通过既有 `accounting.Gate/Session` 接入 source TCP bridge。独立 Engine 不访问数据库、不主动请求控制面。
- 启动时固定一个 snapshot ID 和原始字节有限预算。`source_credit` 要求 source execution point、显式 transport、evidence_file、最大块至少 64 KiB；UDP listener、热 route set、非 source 角色明确拒绝。
- 每方向独立增长预扣块，最大 4 MiB。有限本地 allocator 按配置水位将 unissued 转为 available；消费和未知上界不能重复签发。关闭退回未用 block tail，未决 Batch 仍占 held，unknown 转为独立冻结占用。允许把同一会话另一方向的未用 tail 退回同一额度池，避免余额边缘出现虚假耗尽；不回收其在途 Batch。
- 每次转移保持 `budget = unissued + available + held + used + unknown`，各项非负。Batch 仍只结算一次；部分明确写入按实际结算，歧义短写冻结整个预扣上界。
- source 退出等待 accepted TCP workers 完成，随后写出 `.credit.json` 汇总；写失败导致进程非零退出。目标端 JSONL 继续只作 tracer 计量证据，不能与 source 的授权消耗重复当作财务扣账。
- 无 source_credit 的旧 tracer 仍为诊断模式。当前其他性能脚本没有自动开启新授权模式，不能把旧吞吐数字称为正式授权门控性能。

审查源文件：runtime/accounting/credit.go、runtime/config/config.go、cmd/vortex-relay/main.go。有限测试覆盖守恒、续补、双方向、并发消费/unknown、关闭后在途结算、部分提交/返还、取消和零额度；真实认证 VLink bridge 注入短写/拒绝，验证 unknown 不随 session shutdown 释放、零余额无 target Write。配置测试拒绝会绕过门控的组合。

## 五进程回归

入口：`GOMAXPROCS=1 BACKEND={docker,native} make phase4-runtime-credit`，支持 `credit.py --out <dir>` 保留不同提交结果。

拓扑仍为 source → relay-1 → relay-2 → relay-3 → destination → echo target。测试预算 262,144 原始字节，初始块 16 KiB、最大块 64 KiB、grant target 64 KiB、续补水位 30%。这些是**耗尽实验参数**，不是建议生产默认或 D-04 锁值。

| 检查 | Docker（OrbStack arm64） | 原生 Ubuntu 24.04.5 amd64 |
| --- | --- | --- |
| 代码 SHA / dirty | 6a1faf2 / false | 6a1faf2 / false |
| 请求/响应 | 8 × 16,384 bytes，各方向 131,072 | 相同 |
| 已消费 | 262,144 bytes | 262,144 bytes |
| 本地额度分配 | 4 次 | 4 次 |
| 耗尽后旧连接及新连接 | 都无额外目标字节；2 次拒绝 | 相同 |
| unissued / available / held / unknown / sessions | 全为 0（本正常耗尽场景） | 全为 0 |
| credit 与 recovery checksums | credit 9 / recovery 8 项通过 | credit 10 / recovery 9 项通过 |
| 全量 race | 本机 phase4-m2-check 中通过（Darwin）；另有 Linux Docker 协议检查 | 原生 `go test -race ./...` 通过 |
| 8 组真实进程切路/恢复 | 同提交通过 | 同提交通过 |

unknown 冻结在独立短写注入及并发测试中验证；上表正常耗尽流量的 unknown=0 不代表故障情况下应释放 unknown。

两宿主 credit semantic manifest 相同：`ca16df9a9074cd50d9651a584c3abe05f8de9bad3be10a48bb4bdf02ac8f123c`。部署地址、证书路径导致 exact config hash 不同，均原样留存。probe 在宿主运行，是 TCP 正确性工作负载，不是吞吐 driver 或独享拓扑资格化。

原始位置：本机 `artifacts/phase4/credit/docker-6a1faf2`、`recovery/docker-6a1faf2`；原生 `/root/Vortex/artifacts/phase4/credit/native-6a1faf2`、`recovery/native-6a1faf2`。原生 JSON/日志/hash 清单已复制到本机对应目录，不包含私钥。复核 `checksums.sha256`；完整原生 Engine race 日志位于 `/tmp/vortex-race-native-6a1faf2.log`，本机已保存副本到 `artifacts/phase4/review/6a1faf2-native-race.log`。

## CI 修复与结果边界

上一文档 SHA 956d2b5 的 run 41840426926 在恢复夹具失败：target 尚未监听，初始 readiness echo 遇到 EOF，probe 只捕获 OSError，RuntimeError 直接终止。修复只对 readiness probe 在既有有界 wait 内重试这类短读，不把后续业务断言变成重试或成功。

a048cd4 的 run 41849096326 已主动取消，由包含 readiness 修复的 6a1faf2 新 run 41849553357 替代；取消不计通过。新的 credit step 前置到 shared correctness 后，测试失败可尽早显现。最终 Actions run 41849553357 在干净 6a1faf2 上通过全部步骤，artifact 上传成功。本机 Docker / 原生 Ubuntu / Actions 都通过此有限正确性切片；三者都是回归证据，不扩大为性能资格。

## 尚未实现

这是一份**非持久化、单快照、TCP** 测试 authority。重启会重新创建测试预算，不能用于生产或权威恢复。没有远端签发、请求/grant ID、响应超时/未知/迟到幂等、200 ms 等待、每分钟速率估计、30 秒动态目标、闲置回收、系数换算、跨快照授权切换、UDP source 授权。NFR-039/040/044 和完整 D-04 仍未通过。

下一切片应先扩展可注入签发结果与 snapshot/grant identity，在当前安全边界内接 UDP 并覆盖迟到/重复/未知响应；固定预算的同步 replenish 不能冒充真实 Hub 续申。随后完成时钟/份额/快照运行时验证，再扫 D-04 参数、处理 NFR-007 早期 DATA 和正式资格化。
