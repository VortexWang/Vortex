# CR-008：份额、有限资金运行授权与显式恢复候选

状态：待 owner 明确批准。精确审查单元为同目录 `CR-008-limits-runtime.json`，固定规范文件、wire codec、迁移与生成结果的 SHA-256，并绑定已批准 CR-007 的基线。CR-005/006/007 proposal/approval 和历史治理记录原字节不改写。

候选 JSON SHA-256：`4e29497f82808b9a8693030ef400dae3a2bc42cc27cf8a39f24bdcf8605074bd`。

| 契约 | 候选版本 | 改动 |
| --- | --- | --- |
| database | 2.2.0 → 2.3.0 | migration8：独立份额链、租约/关闭/CAS、持久candidate window、来源资金parent/child、seal/final、Node/Hub显式恢复证明与任务 |
| public API | 2.2.0 → 2.3.0 | 份额准备/交接/查询的期望状态契约；实际HTTP adapter仍06-07 |
| control | 1.5.0 → 1.6.0 | 独立能力协商、份额关闭/CAS、funded Hub journal、信用关闭/完整inventory、Node boot和Hub同库恢复/fence/root/admission |
| runtime / authorization / VLink | 保留已批准版本和字节 | 沿用现有独立权利与来源模型；旧字段编号和选定旧报文编码/解码保持兼容 |

同节点同boot份额交接先预留max(a,b)，关闭完整旧续租签发集合，再Node FULL CAS；匹配实际执行回执后才释放差额，连接降额还需真实排空。丢回执或未知激活持续占用，超时/断连/版本未更新均不是停止证明。

运行/share租约每60秒续签，TTL最多300秒；独立10秒FULL heartbeat维持已有恢复保护。租约刷新不发credit，也不延长旧credit。新的parent只有在同一执行范围的所有predecessor完成完整seal、全部child final和来源级parent final后才可申请；资金从原始逐来源B-F预留，旧未知负债不因expiry或epoch变化消失。

Node新boot和Hub恢复均需显式、当前有效的platform授权与各自停止/安全截止证明；Hub同库恢复还需完整保留prefix、原资金/份额manifest、Node实际安装inventory fence、独立新根及FULL回执和全租户admission。PG admitted本身不是Hub本地ACK或online许可，不能通过手动改blocked状态进入运行。恢复保留原source/limits/deadline；允许花费的只是已证明未签发的有限R。

06-06覆盖与验证入口见 [LIMITS.md](../coverage/phase6/LIMITS.md)。完整本机后端 race 已通过（service334.663秒）。提交 `cf92b7e24b6c557a5d14a18b0549a67eac349eb4` 的干净克隆在 Linux Docker arm64 通过完整 backend normal/race（service211.753/359.668秒）、Agent/Store/Hub/contracts/Engine normal/race、有界磁盘满、ledger/share fault suites 和 Node/Backend/Hub Linux双架构构建。源码前后干净，关键用例和摘要校验通过。原始证据及校验结果位于 `artifacts/phase6/06-06/task3-unified-acceptance/`。最终同SHA Actions配对仍归06-11。

保留的已知观察：更早的开发 Linux 用例曾出现3次连接而非预期2次，原因尚未确定；严格断言未放宽，加入退出诊断后独立3次、完整受影响回归及上述干净克隆的普通/race/fault检查均通过。该失败日志仍保留，不宣称已定位或证明不存在间歇重试。

实际pending Commit恢复案例仅选定成员运行真实Engine；其他固定成员使用prepared fixture和真实容量预留，不声称全成员转发/滚动升级/重启验收。有限性质算例、SIGKILL、FULL回滚不等于完整模型或物理断电。交叉编译不等于native amd64运行。HTTP适配器、跨域全验收、性能和物理恢复保持其后续计划范围。

本候选不创建approval，不冻结新契约，不标记06-06完成。owner批准必须绑定上述精确JSON摘要。
