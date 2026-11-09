# Phase 4 有限覆盖登记

依据 04-CLOSURE.md 的 P4-Q2/Q3；本表区分进程攻击、协议单测和注入
适配器，不要求数据库或生产控制面。历史矩阵不代表当前代码仍缺相同实现。

| 范围 | 可执行证据入口 | 当前边界与剩余项 |
| --- | --- | --- |
| 配置路径 OPEN 拒绝 | tools/phase4/attack.sh、attack_gate.py | TCP/UDP 各 8 类：未知规则、旧/未来修订、旧/未来 generation、跳跃、经过本地的环、重复 visited；另有 UDP 断链。172b0a7 本地五进程 17 项通过。尚需最终构建原生与 Docker/Actions 分开留证 |
| HELLO 身份与签名 | engine/vlink/hello_attack_test.go | 10 个签名转录字段篡改、真实 TLS 上错误签名密钥；属于协议测试，不能替代全部进程身份组合 |
| 完整性、半关闭、断链旁路 | tools/phase4/fault_workload.py、comparison.sh | 64MiB 确定性文件 SHA256、半关闭、断开中继后既有及新流不得绕行；最终构建重跑尚缺 |
| 配置切换与恢复 | tools/phase4/recovery.py、modes.sh | 进程切换/恢复和模式检查；须核对运行配置是否启用相同授权适配器，不将旧诊断结果自动提升为最终资格 |
| 政策时间与快照 | engine/runtime/accounting/policy_test.go、book_test.go | q=T−60、TTL300、保护带恢复、硬期限、时钟回退、跨路径政策切换、迟到授权及旧 unknown 保留；是注入边界证据，最终 tracer 场景覆盖仍需逐项对应 |
| 份额 CAS 与排空 | engine/share/share_test.go、runtime/accounting/share_test.go | 幂等/CAS、重叠最大占用、升级降级、boot fencing、到期禁止新消费及等待中到期退还预留；真实持久化回执与重启属于 X5，不作为 P4 阻塞 |
| 来源与消费守恒 | runtime/accounting/grant_test.go、credit_test.go、book_test.go | 拒绝/未知/迟到、授权 ID 复用、部分写入、取消、跨快照绑定、闲置尾块回收；未知上界不因超时或关闭释放 |

172b0a7 本地 `go test -race ./share ./vlink ./runtime/accounting -count=1`
通过（1.463s / 7.188s / 3.049s）。测试的实际语义和进程配置均为验收
边界；本表不把测试名或绿色汇总当作完整 NFR-006/039/040/044 通过。

当前结论：P4-Q2/Q3 仍待最终覆盖核验；以上为有界清单，不新增“所有
未来攻击”或 Phase 5/6 生产实现要求。
