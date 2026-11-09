# CR-012：Phase 4 回归修复与 Phase 6 契约整合

状态：候选，等待对同名 JSON 及其列出字节的精确批准。此文不代表批准或最终系统验收。

目标为把 Phase6 `c85ed753c8ea92965346f12f5b4e7ea30e900695` 整合到包含 CR010/011 与 F10/F11 修复的主线候选 `729cd8de28c115a2c5aa76b528100a26de4bbf0c`。原用户工作区与未提交修改保持原样。

## 精确契约范围

- control **1.8.0**：完整合并已批准 Phase6 control1.6.0 与 CR010 control1.7.0。保留 NodeMessage16 LifecycleHello、17 NodeReport、18 RenewalRequest、19..24生产回执；ControlMessage15 HubStatus、16 RenewalResult、17 CredentialRevocations、18..23生产命令；实验 CreditDemand / CreditDemandDenied 继续使用字段100。
- 所有原字段描述符、枚举值、oneof 索引、服务方法保持原值。105个描述符恰为两条基线69/100个描述符的并集，候选不增加额外消息定义。
- lifecycle.md 保留 Phase6 全部生产语义并附加 CR010 的非区域实验约束。独立显式 opt-in 实验不进入生产后端配置通道。
- VLink1.1.0 保留 CR011 精确规范，wire0x0100不变。数据库2.4.0、API3.0.0、runtime1.2.0、authorization1.2.0保留 Phase6 已批准字节；没有新增数据库迁移或权限、财务含义。
- JSON包含全部规范、伴随文件、生成Go、catalog/changelog/需求映射摘要、两条基线摘要及全部历史批准/提案摘要。两份diff分别对两条基线，不能仅审查其中一份。

## 实现整合与回归

F10：成功的耐久消费提交后发出有界、可合并的需求唤醒。reserve、return、失败checkpoint与其他revision不唤醒；通知不授予额度。实际写入仍验证原持久化权限与200ms上限。

F11：复用 Phase6 已实现的初始化/实际运行/最终证明context分离；实际故障切换仍限5秒，任意查询失败不能当作“没有记录”。

额外合并回归：Phase4接收侧ACK处理保留流控防死锁，同时恢复 Phase6配置通道拒绝使用量ACK的边界。真实mTLS负向测试确认known/unknown outbox均不能被该通道压掉，使用量hook不触发，余额和未知负债不变。CLI与ControlClient同时拒绝实验需求和生产direct backend混用。

测试夹具收敛：有限DemandLimit按独立请求身份计数，重复重放不提前终止连续续领，响应仍按原耐久身份精确返回。后端到期闭环夹具补真实Hub→backend使用量mTLS确认；Phase5恢复夹具显式提供由原source owner绑定的后继根与原allocation摘要，不放宽生产校验。

SQLite保留两分支全部迁移，完成后才发布合并schema20。FIFO事务帮助接口保留；Phase6aggregate限速/连接份额在原事务中校验，不能因prepared fast path漏掉。

## 检查与证据边界

开发验证原失败日志与修复后结果保留在 `artifacts/phase6-integration/`。已通过的focused结果包括31.196s连续/32轮续领及ACK负向、三轮角色隔离、三轮23.873s真实到期结清续资、全部五个8.702s进程恢复场景。完整native/Linux与clean-source结果以证据清单为准；早期完整root/backend失败不被覆盖。

修正后完整原生root及真实PG/Valkey后端race均通过（service569.990s）。干净提交fb3a9786的Linux arm64完整root/Engine race通过，启用真实4MiB满盘夹具（Agent253.534s、Store258.953s、Phase5进程112.495s）；独立数据库的完整Linux后端race通过（service550.929s），运行前后源码干净。双Protobuf、双sqlc全部33文件一致，六个Linux构建及许可/负向检查通过。精确日志摘要及环境见 `artifacts/phase6-integration/validation-manifest.json`；这些结果不代替批准后相同SHA的最终Docker/Actions验收。

原CR005..011提案和批准记录保持不可变。原CR009 exact checker与篡改测试继续在原完整catalog快照执行；新的forward checker独立校验当前全部受保护字节、两基线描述符并集、需求映射与新批准记录。没有跳过冻结门禁。

最终同SHA本机Docker与Actions、完整Phase5/6入口在冻结后运行；历史Phase6完成证据不自动证明本次组合。Phase7尚未开始。

## 批准动作

批准 CR-012 允许把本JSON列出的control1.8.0候选catalog改为frozen，按当前control及四个规范伴随文件摘要追加一条独立批准记录，保留旧记录；允许完成已授权的Phase6合并与验证。规范候选字节、生成代码、changelog和需求映射不得借批准再次改变。
