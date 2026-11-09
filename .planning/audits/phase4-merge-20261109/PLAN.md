---
status: complete-with-open-findings
workflow: gsd-audit-fix
execution: inline
base: a102bb31d45711acfade8eab9ac4ca7bc3016a78
---

# 合并后审查与修复

用户要求“下一步审查和修复”。针对 main 的合并结果及已知 CI 失败做源码审查、最小复现、修复与验证；保留原工作区。不改写原验收结果，不凭本轮修复授权静默重新批准冻结契约。

| ID | 严重性 | 发现/线索 | 分类 |
|---|---|---|---|
| F-01 | high | VLink 64 MiB 完整性/半关闭失败，DATA 状态违规后链路关闭 | 调查中；复现并确定具体改动后自动修复 |
| F-02 | medium | 并发 funding block 短写测试的 200ms 排队超时 | 调查中；区分行为缺陷与夹具时间假设 |
| F-03 | high | policy-cutover 初始 boot authority 激活超时 | 调查中；不能通过跳过切换掩盖 |
| F-04 | medium | 旧 VLink encoding 精确批准摘要与实际字节不符 | 需明确契约审批；先准备具体提案 |
| F-05 | medium | GSD audit 命令拒绝历史 passed_with_scope 状态 | 工具兼容性；保持历史验收，直接审阅现有证据 |

先读取 UAT/VERIFICATION 与真实 Actions 日志，再增加失败复现，按 finding 原子提交。自动测试门禁失败则保留报告并停止该修复，不提交失败代码。每条通过的修复运行相关 race/真实进程或 Linux 拓扑；最终执行完整入口并按 SHA 记录本机与 Actions，不推断性能资格化。

审查与已确认修复已完成，完整验收仍未通过：F-01/02/03/04/06/07/08/09有独立修复及验证；F-05为外部GSD兼容限制；F-10连续流量授权时限与F-11恢复切换超时保留未解决。PR保持草稿，未经全量通过不合并。初始五项审查之外的F-06/07/08/09来自同路径复现和最终门禁，不扩展为架构或性能重构。
