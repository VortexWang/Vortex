---
status: complete
phase: 03-m1-vlink-m1-vlink-engine
source: 03-01-SUMMARY.md, 03-02-SUMMARY.md, 03-03-SUMMARY.md, 03-04-SUMMARY.md, 03-05-SUMMARY.md, 03-06-SUMMARY.md
started: 2026-11-04T00:00:00+08:00
updated: 2026-11-03T16:57:18.622Z
---

## Current Test

[testing complete]

## Tests

### 1. 认证 TCP 转发与半关闭
expected: TLS 1.3/mTLS VLink 转发完整请求；客户端半关闭后仍收到目标完整响应，连接正常结束。
result: pass

### 2. Mux 流隔离与窗口流控
expected: 同一链路的多个 TCP stream 保持独立；TCP 在窗口耗尽时等待 credit，UDP 超窗丢弃并可在 credit 恢复后继续。
result: pass

### 3. OPEN 授权与 UDP 开流
expected: 目标只由本地策略选择，授权完成前不发送 OPEN_OK；未授权 DGRAM 被拒绝，链路断开后流和目标资源清理。
result: pass

### 4. Pool 与反向建链
expected: 链路池遵守配置和硬上限，失败重试带退避；反向节点主动建立 TLS 链路后，ingress 能在该链路上发起获准 stream。
result: pass

### 5. 协议限流与观测隐私
expected: 持续控制帧违规达到阈值后封链；指标使用低基数标签，结构化事件不暴露地址、租户、规则或 stream ID。
result: pass

### 6. 双宿主验证与性能证据边界
expected: 同一提交的本机 Docker 和 GitHub Actions 检查均通过；Ubuntu KVM 原始基准与哈希可追溯，NFR 数值标为候选结果，并明确 loopback、无正式计量门控及未覆盖生产并发/持续负载。
result: pass

## Summary

total: 6
passed: 6
issues: 0
pending: 0
skipped: 0

## Gaps

[none yet]
