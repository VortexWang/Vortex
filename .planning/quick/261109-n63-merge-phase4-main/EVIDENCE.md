# Phase 4 合并候选：验证与批准边界

状态：`review-ready / awaiting CR-010`。本文件不代表合并已经完成。来源、主线及原 Phase 6 工作区保留，未创建最终合并提交，未推送 main。

- 来源：`phase4-runtime-authority-qualification@2cb3ab5326a4339ec4c4e9e2917073ba869fa5bb`。
- 目标基线：`00704144c17f69dfb0a1cb2f8b1005e5c97e1ad9`；已包含远端 main `7b0363c96c9f3196dc48ce2aec39936caeffc4e4` 的 Phase 5 闭环。
- 候选分支：`codex/merge-phase4-main`，独立工作树 `/tmp/vortex-merge-phase4-main`。
- 提案：[CR-010](../../../docs/governance/proposals/CR-010-phase4-merge-wire-migration.md)（仓库路径 `docs/governance/proposals/CR-010-phase4-merge-wire-migration.md`）；同名 JSON 固定规范、生成物、源码清单及证据报告摘要。
- 机器报告：`artifacts/merge-phase4-main/report.json`；完整候选源码清单：`candidate-source.json`。源码身份摘要不包含证据文档，避免循环 hash。

## 集成结果

主线 NodeMessage 16/17/18 和 ControlMessage 15/16/17 保留。来源实验 CreditDemand/CreditDemandDenied 分别迁至两个容器的字段 100。已核对 Phase 6 已批准字段，100 无碰撞。control catalog 仍为 `1.7.0 candidate`；运行时只声明已实现的 1.3.0 生命周期子集，不宣称 Phase 6 功能。

保留主线会话 fencing、证书吊销、区域 failover、授权/份额/政策来源检查、续领、配置激活与升级恢复功能，叠加 Phase 4 有界早期数据、持久化需求、块/片段额度和 checkpoint。实验需求显式启用，不能混入区域生命周期会话。SQLite schema 20 只在组合迁移全部成功后发布。

真实双进程夹具现在通过统一 runtime 身份 CA、规则 UUID 和配置激活事务。静态 VLink 客户端按 Node UUID SAN 正常验证证书；没有禁用 TLS 身份检查。配置 ACK 后仅显式 DemandIssuer 夹具保持需求流；默认行为保留。

## 已执行验证

| 范围 | 结果 | 证据（均在 artifacts/merge-phase4-main） |
| --- | --- | --- |
| Darwin root 全模块 race | 通过；在最后实验单调速率开关及夹具修复前执行，后续覆盖分别列出 | root-complete-final-race.log.gz |
| Darwin Engine 全模块 race | 通过；此后 Engine 源码未变 | engine-race.log.gz |
| Linux Store / identity / upgrade / Hub / contracts / CLI / test/phase5 race | 各 package 通过；同轮 Agent 曾失败，不能把原整轮标为通过 | linux-final-race.jsonl.gz |
| 修复后的 Linux 完整 Agent race，随后完整 Engine race | 通过，Agent 284.521 秒 | linux-agent-engine-final.jsonl.gz |
| Linux 长期续领、持续续领及块传输 | 通过，151.198 秒 | linux-long-fixed.log.gz |
| 最终迁移/负向旧 wire/能力隔离、时钟/速率边界 | 通过 | final-boundary-corrected-race.log.gz |
| 最终 Darwin Hub、Node/test-Hub CLI、contracts race | 通过 | fixture-final-race.log.gz |
| 最终 go vet Agent / Store / Hub / CLI | 通过 | fixture-final-vet.log.gz |
| Linux 真实 tmpfs 磁盘满：升级停止、交接事务 | 通过 | linux-full-disk.log.gz |
| Python 资格/攻击/审计工具 | 37 项通过 | qualification-python.log.gz |
| 三中继 Docker smoke、Linux VLink 脚本 | 功能通过，不作性能资格化 | docker-topology.log.gz / vlink-linux.log.gz |
| Darwin 独立 Node/test-Hub 四项进程测试 | 通过，47.241 秒 | processes-active-report.json / processes-active-test.log.gz |
| Linux arm64 Docker 同四项进程测试 | 通过，56.269 秒 | processes-linux-mounted-report.json / processes-linux-mounted-test.log.gz |
| Node / test-Hub / relay Linux amd64+arm64 构建 | 6 个制品成功，只证明构建 | cross-build.json |
| Protobuf 两次独立生成与工作区逐字节比较 | 通过 | generation-final.json |
| 主线字段保留及 Phase 6 字段不碰撞 | 通过 | wire-compatibility.json |
| SemVer、changelog、需求映射、原授权与 runtime 冻结检查 | 通过 | contract-change.json / requirements-map-final.json / authorization-frozen.json / runtime-frozen.json |
| control 全冻结检查 | **预期未通过**，新字节未获精确批准 | contract-freeze-pending.json |
| 本候选 GitHub Actions、完整 Phase 5 clean-source 发布入口 | **未运行** | report.json |

主要入口：root 与 Engine 模块的 `go test -race ./... -count=1`；最终 focused `go test -race ./hub ./cmd/vortex-test-hub ./cmd/vortex-node ./contracts/... -count=1`；`go vet ./agent/... ./hub/... ./cmd/...`；`tools/phase4/durable-process.sh`；两个架构的 `CGO_ENABLED=0 GOOS=linux GOARCH=... go build`。进程脚本自行构建两个真实二进制，四项分别验证 TCP、UDP、16 流并发及超出初始 8 MiB 的持续自动补充；最终校验 Agent、Hub、协议后端用量与原资金守恒。

环境：Go 1.27.3；Darwin arm64；Docker Server 29.8.2 arm64；Linux 工具镜像 `golang@sha256:e519d3b0a5b285e5aa53ab9eef02bc6bbb9cb04608389b743a136a9ce75f0d1e`。生成器 protoc 36.2、protoc-gen-go 1.36.12、protoc-gen-go-grpc 1.6.2。

## 保留的失败与修复

早期失败日志均保留，详见 report.json 的 retained_failures。包括 TLS helper 未转发 Hub 身份、schema 版本/升级预检未整合、continuous 测试对 pending 数量作过强假设、旧 wire 测试预期错误码不精确，以及 Linux 保守时间区间导致实验速率永久停滞。最后一项通过原有 boot 绑定单调速率机制修复；授权有效期仍逐次使用保守边界，未放宽租约或未知负债限制。

真实进程夹具还发现旧 `rule-1` 不符合 UUID schema、旧 `vortex` TLS 别名不匹配主线 UUID SAN，以及实验 Hub 配置 ACK 后提前断流。均修复后重新执行四项完整进程测试。第一次 Docker 进程入口缺工作树 Git 元数据挂载而失败；补只读挂载后通过。失败结果不删除、不改写为通过。

测试产生的私钥、SQLite 与可执行文件已移到本机私有目录 `/tmp/vortex-phase4-merge-raw-20261109`，目录权限 0700，不在候选树中。候选只保留压缩文本日志、摘要及校验值；raw-evidence-inventory.json 索引原始文本证据。

## 证据限制与下一步

本轮是尚未提交候选上的分步回归，不是同 SHA 本机 Docker / Actions 配对。Linux amd64 仅交叉构建；未证明原生 amd64 执行、专用硬件性能或 24 小时稳定性。有限测试不是完整模型验证。协议后端夹具不是 Phase 6 PostgreSQL 生产后端；进程测试没有行使证书续期。

原 Phase 4 D-04/Q1..Q5 结论只绑定其已测 Engine/tracer 源码与参数。保留源分支最终审计，不能转移为合并候选的性能资格化。

待所有者批准 CR-010 后，才更新精确冻结记录、运行治理检查，创建含目标与来源两父提交的合并，验证两者祖先关系并推送 main，最后记录 quick SUMMARY/完成状态。批准前不生成 complete SUMMARY。

## CR-010 批准落实（2026-11-09）

用户明确回复“批准 CR-010”。同名 JSON SHA-256 `1f63dabdd16939de49153891248269192650bfb197806e556adf40ddeb931efa`、提案及全部规范/生成物摘要逐项匹配。规范字节未变；catalog、需求映射、changelog 与治理记录已记录批准。原提案与候选报告保留原字节作为审阅快照。

批准后 `tools/phase5/contracts_check.sh`（CONTRACT_BASE=00704144）通过，包含两次独立生成、冻结/映射/SemVer 和契约测试。日志：`artifacts/merge-phase4-main/contract-freeze-approved.log`；批准记录：`approval-CR-010.json`。先创建两父合并提交，再运行 clean-source Docker 验证并推送；最终结果另记 SUMMARY。
