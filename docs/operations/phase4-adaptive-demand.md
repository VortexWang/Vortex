# Phase 4 自适应授权候选运行配置

`vortex-node --credit-demand-config /path/to/demand.json` 显式开启 control
1.7.0 候选的字段 100 持久化续申发送器。该候选待精确批准；旧 Phase 4 实验 peer 的字段 16/15 不兼容，须停止旧夹具并同时更新两端，不使用版本号猜测或自动回退。未指定该选项时保持关闭。必须同时指定 `--hub`
和 `--hub-name`；Hub 的认证证书身份须与配置 HubID 完全一致。配置不是
授权，不能替代 Hub 的持久化 delegation、资金来源、当前 boot 绑定以及
Agent 的实际运行许可。

该路径仅连接显式启用 DemandIssuer 的非区域实验夹具，不能与 Phase 5/6 区域生命周期控制流混用；主线区域控制仍协商 status-v1/renewal-v1。

以下仅为 D-04 实验候选示例，不是锁定默认值。所有 UUID、规则与版本须
替换为实际预置的值。文件不填写 BootID，启动时绑定随机生成的当前 boot；
NodeID 可以省略，填写时必须与 `--node-id` 一致。

```json
{
  "interval_ms": 1000,
  "demands": [{
    "scope": {
      "HubID": "550e8400-e29b-41d4-a716-446655440003",
      "DelegationID": "550e8400-e29b-41d4-a716-446655440004",
      "SourceID": "550e8400-e29b-41d4-a716-446655440005",
      "SnapshotID": "550e8400-e29b-41d4-a716-446655440006",
      "RuleID": "rule",
      "SignerEpoch": 1,
      "RuleRevision": 2,
      "PathGeneration": 3
    },
    "policy": {
      "Minimum": 8388608,
      "MaxTarget": 67108864,
      "TargetSeconds": 30,
      "RenewPercent": 30
    },
    "ttl_seconds": 60
  }]
}
```

文件上限 1 MiB，1–64 个不重复的完整范围；`interval_ms` 为 100–60000。
TTL 为 1–300 秒。金额单位为 raw bytes，Minimum 必须大于零且不超过
MaxTarget；MaxTarget 不超过有符号 64 位上限。TargetSeconds 为 1–60，
RenewPercent 为 1–99。没有隐含候选参数默认值。加载失败在 Engine 启动前
退出，但 Node 初始化已执行旧 boot 授权封存与未完成用量恢复。

Hub 必须先通过已认证控制连接发送当前 boot 的有效 AuthorizationUpdate。
Agent 只有完成授权持久化、严格范围检查并建立保守时间区间后才自动计划
首个需求。首次有效时间建立必须在 boot 后 300 秒内完成；后续可信时间
也受 300 秒新鲜度约束。不提供绕过可信时间的开关。当前配置入口不负责
向 Hub 预置 delegation 或生成启动授权；测试部署必须显式完成这些步骤。

重启后使用新 boot 的配置范围；旧 boot 的请求、授予及未知负债不会因此
删除或重新作为可用额度。实际资金仍由 Hub 事务签发，Agent 收到成功更新
后在同一事务内安装并完成对应需求。拒绝回执与重试退避也持久化。

本入口尚不启用自动闲置收缩/回收。封存、未使用额度证明、Hub 一次性返还
与未知负债冻结需要完整闭环；不能凭计时器到期释放资金。需要以同一干净
SHA 的专用 Ubuntu 运行证据进一步验收，不把配置解析测试作为性能证据。
