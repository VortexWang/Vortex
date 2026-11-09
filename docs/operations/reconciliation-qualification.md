# Phase6 后端对账与最终证据

`tools/phase6/reconciliation_1000.sh` 执行真实 PostgreSQL 工作负载。隔离 schema 中通过现有生产服务持久化1000个确定规则、2000个固定成员、不可变修订/计费快照/编译产物和查询任务。测试适配器为每个精确 transaction/node/boot/revision/fence/artifact 返回确定的 active 观察；它不运行1000个Engine或真实网络。原始Commit答复设为unknown，生产RuleWorker必须读取同一持久事务的查询结果，使全部1000条头部与2000成员收敛，资金/修订/产物不变、占用不释放。

计时从全部输入/查询意图已持久化后开始，包含生产lease、读取、确定适配器调用、回执/审计/Outbox事务、最终PG收敛计数。准备fixture、最后不可变历史比较不计入正常对账时间。严格按纳秒检查≤60秒，少一条规则/成员、错身份、未知/超时或超限都失败；失败保留JSON。此工作负载验证有限后端对账路径，不替代真实Node/Hub/Engine、数据面吞吐、专用硬件或Phase8系统资格。

原worker每次成功任务后都等待100ms，2000成员至少需200秒。新生产Run按32项有界批次处理，队列暂空时等待100ms；启动和每秒执行一次有界恢复扫描，不再每个成员重复全局扫描。每项仍使用持久租约与匹配回执。

Prepare/Commit先提交task-only lease，释放任务锁后按tenant→task复核token、state、期限及当前权利，再同事务提交running。等待期间被superseded或换租约的任务不能发送。未发送且权限/份额暂不可用时保留pending，返回“无工作”避免忙循环。Query/Abort不产生新激活权利，但事件外键仍会锁tenant，因此同样先提交task-only lease，再按tenant→task复核；receipt/deployment使用tenant→head→deployment/task顺序。两个提交之间崩溃留下leased未知项，原Recover按过期租约产生unknown和同事务查询，不释放占用。取消、持久化/传输错误和未知结果语义保持原边界。

## 最终同SHA入口

在独立干净checkout执行（输出必须位于源码外）：

```sh
./tools/phase6/check.sh --through-plan 06-11 --expected-sha "$SHA" --produce docker --evidence-dir "$EVIDENCE"
./tools/phase6/evidence_manifest.sh --through-plan 06-11 --expected-sha "$SHA" --evidence-dir "$EVIDENCE"
```

producer使用隔离Compose项目，实际Linux容器执行完整backend/root/Engine normal与race、两个1000规则原始JSON、磁盘满/账本/share/recovery故障、真实进程、双生成器校验、六项cross-build、依赖许可和精确CR-009冻结。完整96需求归属必须与计划一致。macOS不是Linux资格；amd64/arm64交叉编译不是两种CPU执行。

Actions矩阵分别使用ubuntu-24.04与ubuntu-24.04-arm。下载精确SHA的两个artifact到`actions-amd64/`和`actions-arm64/`，本机放`docker/`；pair gate逐文件校验摘要、Go JSON语义、实际架构、源码状态和SHA文件cmp，任一缺失/跳过/失败/超时均不通过。

真实CloudflareCRUD沿用受保护专用测试zone。可在本机导出`PHASE6_CLOUDFLARE_ENV_FILE`指向0600环境文件；其中policy文件使用绝对可读路径，不复制到源码或artifact。Actions使用明确配置的加密secrets：CLOUDFLARE_API_TOKEN、CLOUDFLARE_ZONE_ID与CLOUDFLARE_TOKEN_POLICY_JSON（或独立只读CLOUDFLARE_POLICY_TOKEN）。本轮已获用户明确授权并配置三项测试secrets。未配置时保留not_run；即使runtime通过，acceptance_complete仍false，最终pair拒绝。

宿主预检执行已有Ruby/OpenAPI、精确批准及依赖许可检查；Go镜像内独立执行sqlc/protobuf双生成。报告时间采用Go RFC3339Nano原字节，宿主Python按整数纳秒解析，兼容Python3.9并保留0..9位精度。输出权限交回调用者UID/GID，保留0600及原测试退出码。

VLink历史Phase3文档与原review hash不一致，原文/版本未改；单独保留失败日志，不把Phase6五项契约冻结冒充历史全局冻结。Phase4专用资格、Phase7运维/前端和Phase8完整验收不在本producer通过范围。

最终worker锁序修正：operation/domain-event/audit外键本身会请求tenant锁，因此query/abort也必须释放task-only lease后再按tenant→task重验。Port release仅从原immutableartifact重构stop，不重新授予激活；projection按tenant→group→allocation→task。Control expiry用冻结query的回滚发现扫描，在tenant→task锁内原子提交unknown/task+operation，失败后仍可发现重试。六路径旧代码RED55P03、八路径与expiryfault修复GREEN，修复提交f54c8017的旧验收轮次单独保留；当前最终三端结论见下文。

Agent续资发送也采用有界排空：每个既有retry turn最多4条原不可变journal请求，连接局部游标轮转未响应项，重连仍从原ID重放。防止大量FULL续资工作使串行心跳和回执饥饿；不改签发、资金、watchdog或20秒收敛门槛。修复提交f466bfab的Actions42565204602旧轮次保留；当前最终验收提交见下文。

Hub状态观察先固定SQLite只读快照，读完状态再采样可信时间，避免新心跳并发提交引发误报blocked。未知时钟、futureheartbeat、原截止及实际授权签发检查保持独立。当前最终实现65a5335894732b7d632e870acc3d8283be224cd3的本机Docker、Actions42572674719原生amd64/arm64全部通过，含真实CloudflareCRUD；千规则普通/race分别8.567/13.649、23.835/32.862、29.746/42.092秒。完整范围与历史债务见06-VERIFICATION.md。
