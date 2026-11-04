# Grant 结果与 UDP source 门控 — 2026-11-04

代码：`d2f21f8d444781aee43838011b3f6154e3fb4498`，本机 Docker / 原生 Ubuntu 使用相同干净提交。Actions [41856742608](https://github.com/VortexWang/Vortex/actions/runs/41856742608) 在同干净 SHA 全步骤通过：新增 credit、全量 race、TLS/故障、混合负载、scale、pool/mux/auto、恢复及 artifact 上传。Phase 4 保持 executing；这里是有限非持久化执行切片，不是完整 NFR/D-04 放行。

## 签发请求与结果

在 `runtime/accounting/grant.go` 中新增可信进程内注入接口 `BeginGrant` / `ApplyGrant`。每个 Gate 仍只有启动时固定的 snapshot 和预算，Engine 不主动访问控制面或数据库。

- 请求先把金额从 unissued 转到 pending；request ID 绑定原金额，重投不再占用。
- 回执必须匹配 request、snapshot、amount。accepted 要求非空 grant ID；同一 grant ID 不能属于另一个请求。重复相同结果只应用一次，accepted/rejected 终态不能复活或改写。
- unknown issuance 单独占用 unknown_grant，不是已确认可用额度。只有匹配的 accepted 迟到结果，或可信注入方明确断言“未签发证明”，才能转出。elapsed time 本身不是证明。
- 真实写入的 unknown 与签发未知独立保留。统一守恒式：`budget = unissued + pending_grant + unknown_grant + available + held + used + unknown_write`。
- 请求记录与活跃 session 各最多 4,096 个，不删除旧请求来允许 ID 复用；本地自动 allocator 的 `local:` 命名空间不可被外部注入冒用。达到记录上限后不继续签发新额度。
- 默认 allocator 仍只是同步、有限的本地已知成功分配，经同一状态转换处理。没有远端 Hub、网络请求、自动超时回调或持久化。`NotIssuedProof` 是可信进程内调用方的断言，不是本切片新增的密码学证明或外部 API。

race 测试覆盖 pending/unknown、重复并发回执、迟到 accepted、拒绝未知释放、明确非签发证明、快照/金额/request 不匹配、grant ID 复用、终态复活、请求记录上限、取消，以及写入 unknown 后重投原 accepted 不增加可用额度。

## UDP 与会话生命周期

source UDP 与 TCP 共用注入的有限 Gate；不再禁止 UDP listener，仍禁止热 route set 和跨快照切换。每个 UDP flow 在 OPEN_OK 后取得 accounting session：

1. 向 VLink WriteDgram 和向本地客户端 WriteTo 之前都 Reserve。
2. VLink 明确队列拒收时 Return；网络短写/错误时 MarkUnknown；完整成功写入按长度 Commit。
3. 结束先取消/Reset 并 join 接收 worker，再 Close 额度 session，避免接收协程与退出结算竞争。
4. 额度紧张时可从同一静态 authority 的其他 session 退回未用块尾量；在途 Batch 和 unknown 不参与回收。新增测试验证尾量回收不会重新签发 unknown。

`authorizedDatagram` 的拒绝/成功/队列拒收/短写/零网络写入/错误测试均通过。中继/出口仍仅提供计量观测，不把它们重复视为 source 财务扣账。

## 实际进程 workload

入口：`GOMAXPROCS=1 BACKEND={native,docker} make phase4-runtime-credit`。现在依次运行 TCP 与 TCP/UDP 交错案例，两次分别启动真实五进程三中继拓扑，各自拥有一份 262,144-byte 测试预算。

| 案例 | 已授权用量 | 耗尽检查 | Docker / Ubuntu 结果 |
| --- | --- | --- | --- |
| TCP | 8 × 16 KiB 请求及响应，总计 256 KiB | 旧 TCP 和新 TCP 无额外 target 字节 | pass / pass |
| UDP + TCP | 64 × 1 KiB UDP echo；在第 32 次后插入 4 × 16 KiB TCP echo，总计 256 KiB | 旧 UDP、新 UDP 与新 TCP 均无额外 target 字节 | pass / pass |

两宿主均 dirty=false，结束 used=262,144，unissued/available/held/pending_grant/unknown_grant/unknown_write/sessions=0。正常成功案例 unknown=0 不代表故障未知应释放；故障保留见独立注入测试。混合案例本地 grant 分配次数是 5，属于分配时机和块尾回收的观测，不是必须固定的业务不变量。

- TCP semantic manifest：`6a61920a520456c91c0aee1faea1e016e49f440ea7a85017f782cce5f1ca1296`。
- UDP 交错 semantic manifest：`073b25d8ef8ec6befd1cc5b1b8aaa6cf32531491f93a2ca0cca84bb35ed4a392`。
- 同语义 workload 的 Docker/Ubuntu hash 一致；exact config hash 按各自地址/证书路径记录，未要求一致。
- Docker 本机路径：`artifacts/phase4/credit/docker-d2f21f8-r2`、`docker-udp-d2f21f8`。
- 原生完整路径：`/root/Vortex/artifacts/phase4/credit/native-d2f21f8`、`native-udp-d2f21f8`；JSON、logs、hash 清单复制到本机同名目录，不复制私钥。
- 本机 `DOCKER_CONTEXT=orbstack make phase4-m2-check` 和 Ubuntu `cd engine && go test -race ./...` 均通过，日志归档到 `artifacts/phase4/review/d2f21f8-*`。

Docker 第一轮 `docker-d2f21f8` 在基础镜像 token 获取阶段遇到 auth.docker.io EOF，没有运行 workload。该 fail 报告保留，不记通过；在未改代码的同 SHA r2 完成重试并通过。开发期间一个新尾量测试曾因 Reserve(27) 后误写 Commit(63) 而失败，修正测试并重跑后通过；没有放宽守恒/未知断言。

## Actions 原始结果复核

从成功 run 的原始日志解析出 TCP / UDP credit-result，均为同 SHA、
dirty=false。used=262,144，pending/unknown/held/sessions=0；TCP grants=4、
denials=2，UDP grants=5、denials=3，与两个宿主一致。两种 workload 的
semantic hash 也分别一致。CI metadata、原始日志和提取后的 JSON 保存于
`artifacts/phase4/review/d2f21f8-actions{.json,.log,-credit.json}`。
这证明的是相同有限测试结果，不将其变成 NFR 正式资格。

## 剩余范围

本切片没有真实 Hub 通信、200 ms 网络授权等待、可恢复持久化账本、速率估计/30 秒动态 target、闲置回收、路径系数换算或运行时多快照切换。标准性能 workload 仍未开启此 Gate，不能将旧吞吐数字称为正式授权性能。签发结果注入证明的是有限进程内状态转换；不是远端签发和网络丢回执系统验收。

下一项先补 runtime immutable snapshot/grant binding 与切换隔离，再处理 NFR-039 时钟租约、NFR-040 share CAS/排空、NFR-044 系数/逐来源占用等运行时证据；之后才做 D-04 参数扫描和 NFR-007 早期 TCP DATA/受控 RTT。D-04 仍未锁定，正式性能资格仍未通过。
