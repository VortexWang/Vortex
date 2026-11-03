---
status: testing
phase: 02-m0-m0-engine-dataplane
source: 02-01-SUMMARY.md, 02-02-SUMMARY.md, 02-03-SUMMARY.md
started: 2026-11-03T16:46:00+08:00
updated: 2026-11-03T16:46:00+08:00
---

## Current Test

number: 1
name: TCP 半关闭与反向响应
expected: |
  一侧发送完毕并半关闭后，请求完整到达目标；反向连接仍能发送完整响应，客户端能读完响应后正常结束。
awaiting: user response

## Tests

### 1. TCP 半关闭与反向响应
expected: 一侧发送完毕并半关闭后，请求完整到达目标；反向连接仍能发送完整响应，客户端能读完响应后正常结束。
result: pending

### 2. Linux TCP splice 路径
expected: 本机 Linux 容器与 GitHub Actions 在同一提交上均执行 splice 专项用例，数据完整且无重复计量；用例名为 `TestTCPPlatformSelectsSpliceForBareTCP`。
result: pending

### 3. UDP 多客户端请求与响应隔离
expected: 两个客户端分别向同一转发入口发送数据；目标端看到独立上游会话，回复各自回到原客户端，不串线。
result: pending

### 4. UDP 空闲超时与取消清理
expected: 零值配置采用 60 秒默认空闲超时；空闲会话过期后可重新建立，取消 Engine 后监听与上游会话及时关闭。
result: pending

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps

<!-- No user-reported behavioral gaps. -->
