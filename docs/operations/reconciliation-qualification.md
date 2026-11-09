# Phase6 后端对账与最终证据

`tools/phase6/reconciliation_1000.sh` 执行真实 PostgreSQL 工作负载。隔离 schema 中通过现有生产服务持久化1000个确定规则、2000个固定成员、不可变修订/计费快照/编译产物和查询任务。测试适配器为每个精确 transaction/node/boot/revision/fence/artifact 返回确定的 active 观察；它不运行1000个Engine或真实网络。原始Commit答复设为unknown，生产RuleWorker必须读取同一持久事务的查询结果，使全部1000条头部与2000成员收敛，资金/修订/产物不变、占用不释放。

计时从全部输入/查询意图已持久化后开始，包含生产lease、读取、确定适配器调用、回执/审计/Outbox事务、最终PG收敛计数。准备fixture、最后不可变历史比较不计入正常对账时间。严格按纳秒检查≤60秒，少一条规则/成员、错身份、未知/超时或超限都失败；失败保留JSON。此工作负载验证有限后端对账路径，不替代真实Node/Hub/Engine、数据面吞吐、专用硬件或Phase8系统资格。

原worker每次成功任务后都等待100ms，2000成员至少需200秒。新生产Run按32项有界批次处理，队列暂空时等待100ms；每项仍走原事务租约与匹配回执。未发送且权限/份额暂不可用的回滚返回“无工作”，避免忙循环。取消、持久化/传输错误和未知结果语义保持原边界。

## 最终同SHA入口

在独立干净checkout执行（输出必须位于源码外）：

```sh
./tools/phase6/check.sh --through-plan 06-11 --expected-sha "$SHA" --produce docker --evidence-dir "$EVIDENCE"
./tools/phase6/evidence_manifest.sh --through-plan 06-11 --expected-sha "$SHA" --evidence-dir "$EVIDENCE"
```

producer使用隔离Compose项目，实际Linux容器执行完整backend/root/Engine normal与race、两个1000规则原始JSON、磁盘满/账本/share/recovery故障、真实进程、双生成器校验、六项cross-build、依赖许可和精确CR-009冻结。完整96需求归属必须与计划一致。macOS不是Linux资格；amd64/arm64交叉编译不是两种CPU执行。

Actions矩阵分别使用ubuntu-24.04与ubuntu-24.04-arm。下载精确SHA的两个artifact到`actions-amd64/`和`actions-arm64/`，本机放`docker/`；pair gate逐文件校验摘要、Go JSON语义、实际架构、源码状态和SHA文件cmp，任一缺失/跳过/失败/超时均不通过。

真实CloudflareCRUD沿用受保护专用测试zone。可在本机导出`PHASE6_CLOUDFLARE_ENV_FILE`指向0600环境文件；不要复制到源码或artifact。Actions使用明确配置的加密secrets：CLOUDFLARE_API_TOKEN、CLOUDFLARE_ZONE_ID与CLOUDFLARE_TOKEN_POLICY_JSON（或独立只读CLOUDFLARE_POLICY_TOKEN）。未配置时保留not_run；即使runtime通过，acceptance_complete仍false，最终pair拒绝。既有本机token不会自动上传GitHub。

VLink历史Phase3文档与原review hash不一致，原文/版本未改；单独保留失败日志，不把Phase6五项契约冻结冒充历史全局冻结。Phase4专用资格、Phase7运维/前端和Phase8完整验收不在本producer通过范围。
