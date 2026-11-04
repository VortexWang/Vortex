---
status: complete
phase: 04-m2-m2-multi-hop-engine
source: 04-01-SUMMARY.md, 04-02-SUMMARY.md, 04-03-SUMMARY.md, 04-04-SUMMARY.md, 04-05-SUMMARY.md, 04-06-SUMMARY.md
started: 2026-11-05T00:00:00Z
updated: 2026-11-04T17:51:25.808Z
---

## Current Test

[testing complete]

## Tests

### 1. 冷启动三中继转发
expected: 从干净状态启动五进程拓扑后，TCP 半关闭仍能收到完整响应，UDP payload 能经授权的三中继路径到达目标；测试结束时进程或容器均被清理。
result: pass
reported: "下一步"

### 2. 目标写入与计量对账
expected: 成功写入的 TCP 字节和 UDP 数据报与目标接收计数及 accounting evidence 对账一致；未确认的写入结果继续保留额度占用。
result: pass
reported: "下一步"

### 3. 授权路径切换与会话排空
expected: 只有预配置且健康的候选路径可激活；新 generation 拒绝旧代新流，既有 TCP 在旧路径排空，旧 UDP 会话结束且不会重放。
result: pass
reported: "下一步"

### 4. Pool、mux 与 auto 模式
expected: 三种配置模式均只使用已授权路径并完成 TCP/UDP 转发；auto 的 TCP 模式选择有界，UDP 保持 mux。
result: pass
reported: "下一步"

## Summary

total: 4
passed: 4
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

<!-- YAML format for plan-phase --gaps consumption -->
