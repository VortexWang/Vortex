# DNS 持久化任务消费

`serve-http` 的 DNS 重算接口只在 PostgreSQL 中提交期望快照、任务、Outbox 和审计。独立的消费进程负责外部 DNS I/O，部署时使用同一数据库并启动：

```sh
vortex-backend run-dns --keyring-file /run/secrets/vortex-dns-keyring.json
```

沿用后端 `PHASE6_DATABASE_URL` 数据库配置。进程只使用固定 Cloudflare API 地址和已登记的加密 zone access；不接受客户端提供的 provider endpoint 或明文 token。凭据登记及 zone/policy 核验仍由现有受信任的凭据登记流程处理。本命令不会创建凭据。

密钥环由部署秘密管理器挂载，格式为 `{"current":2,"keys":{"1":"<旧32字节密钥的标准Base64>","2":"<当前32字节密钥的标准Base64>"}}`。文件必须是常规文件，禁止 group/other 权限，最大64KiB；最多16个正整数版本。配置只包含加密密钥，DNS token 保存在既有 PostgreSQL 加密凭据表。保留仍被密文及备份引用的旧密钥版本。错误不会回显文件内容或密钥。

消费器仅处理带不可变原始 actor 绑定的新 DNS intent，每次从 PostgreSQL 重建当前角色及账号状态，核对目标 domain/access 绑定，并在外部调用前重读真实 serving 集合。历史 actorless 通知不能生成消费权限。任务 lease、审计与状态提交后才允许外部调用；并发进程使用同一任务 lease，已成功任务不重复执行。

provider 超时、响应丢失或 serving 集合变化进入 `unknown`，不释放端口或财务占用。unknown 不会自动高频重试；管理者先读取当前 REST 状态，再以新的 `Idempotency-Key` 重算，provider 按名称/归属查询来收敛。相同 key 重放已接受的原始快照和 operation_id，即使健康状态已经变化；它不会生成新的 DNS 意图。SIGINT/SIGTERM 取消消费者，未确认的外部执行继续按 unknown/过期 lease 处理。

本轮本机/Linux 测试使用真实 PostgreSQL、加密凭据存取与受控 provider fixture；真实 Cloudflare CRUD 的既有证据独立记录，不能把 fixture 视为真实 provider 验收。
