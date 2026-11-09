# 持久化恢复推进

平台人员可在明确选择的 tenant 下读取既有恢复记录：

- `GET /api/v1/tenants/{tenant_id}/node-recoveries/{recovery_id}`
- `GET /api/v1/tenants/{tenant_id}/hub-recoveries/same-store/{recovery_id}`
- `GET /api/v1/tenants/{tenant_id}/hub-recoveries/cold/{recovery_id}`

读取同事务审计，返回业务资源、恢复模式、已持久化阶段及原批准操作（若原记录存储了该关联）。冷 Hub 历史记录没有该关联，不推测最近操作。阶段只说明 PostgreSQL 中的事实；`admission_recorded` 不代表 Hub 本地已完成 FULL、Engine 正在转发，或旧债务可以释放。

平台管理员可提交 `POST /api/v1/tenants/{tenant_id}/hub-recoveries/same-store/{recovery_id}/advances`，携带 `Idempotency-Key` 和 JSON `{}`。响应202包含精确 `operation_id` 和操作查询 Location。同一租户、键、恢复记录及管理员的重试返回原操作；改变绑定返回冲突。普通租户、operator 和 auditor 不能请求推进。接口只接受既有批准记录，不接受 boot、epoch、backend、原始证明或控制命令。创建新的恢复批准仍使用既有受信任管理流程。

`serve` 与 `serve-http` 都运行 PostgreSQL 恢复推进消费者；多实例通过有期限任务租约协调。消费者在领取时重新读取当前管理员身份，使用原批准中不可变的 backend 绑定，并调用每次最多32条命令的既有协调器。等待 Hub manifest、Node FULL 围栏或新根回执时，任务至少一秒后继续；网络交付由现有控制消费者执行。消费者不会生成成功回执来跳过等待。

任务领取、结果、业务操作状态与审计采用一致的租户优先锁顺序。结果审计失败保留未确认租约；租约到期后可按既有不可变命令重新推进。协调器错误记为 `unknown` 并停止轮询，调查后用新键提交新的推进意图。完成只标记该推进操作成功及 Outbox 已交付，原资金占用保持不变；关闭结清仍要求各自的真实证据。
