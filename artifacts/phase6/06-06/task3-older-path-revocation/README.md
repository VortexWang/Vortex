# 旧 path 纯撤销：候选执行证据

范围：Node 已进入新 path/fence 后，经过认证的旧范围 pure revocation 仍可停止旧权利。已知 ID 必须属于同 issuer/boot/path；未知 ID 只写不可变范围墓碑。当前三个准入 floor、heartbeat、资金、已知和未知用量不变。更低 signer epoch、混合 grant/handoff/policy/share refs 和其他已知范围均拒绝。

- 完整 root race：Darwin arm64 Agent72.371s/Store105.377s；Docker Linux arm64 Agent134.403s/Store203.134s；Hub/contracts/Node CLI 同批通过。
- 最终受影响后端 race：native67.318s、Linux67.277s；真实恢复、续资、pending Engine Commit、信用到期及规则端口链路通过。
- 六个 Linux Node/Backend/Hub amd64/arm64 交叉构建通过；不代表 native amd64 执行。
- 实际 mTLS 新测试使用 production issuer/Node consumer + HubTestServer。其独立新路径安装依赖显式本地 journal ACK/CompleteRecovery；这不是 PG admission/financial final，也不是 production RuntimeServer 全恢复证明。后者由独立 backend integration 证据覆盖。

`red-native.log` 保留原实现拒绝旧范围 pure revocation 的真实 RED。`initial-mtls-barrier-native.log` 保留未确认 journal 阻止 fixture 恢复的失败；fixture 后来显式确认，不放宽生产 barrier。

第一次 Linux affected suite 在丢一次 FULL 响应用例中观察到3次连接而非2次；无 watchdog timeout。原因尚未确定。保留 `affected-backend-linux-race.log` 为失败；新增服务端退出原因诊断，独立3次和最终整个 affected suite 均通过，原严格断言保持。不得将该间歇失败改写为已定位。

开发 checkout 证据；完整 backend race 仍继承936edcab，最终06-06统一检查尚待执行；没有同SHA clean-source Docker/Actions 配对、CR-008 approval 或06-06完成声明。Phase6仍8/11。
