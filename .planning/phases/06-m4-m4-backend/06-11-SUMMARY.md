---
phase: 06-m4-m4-backend
plan: 11
subsystem: acceptance
tags: [postgres, reconciliation, evidence, race, cloudflare]
requires:
  - phase: 06-07
    provides: approved REST/OpenAPI and complete backend domain producers
provides:
  - Strict real-PostgreSQL 1000-rule and2000-member reconciliation within60seconds
  - Complete exact-source Docker and native amd64/arm64 Actions evidence with real Cloudflare CRUD
  - Fail-closed semantic/hash/ownership/native-architecture pairing and retained failed attempts
affects: [phase-7, phase-8]
tech-stack:
  added: []
  patterns: [bounded worker drain, tenant-first event locks, renewal control fairness, immutable evidence]
key-files:
  created: [backend/internal/service/reconciliation_1000_test.go, backend/internal/service/rule_worker_lock_order_test.go, backend/internal/service/worker_event_lock_order_test.go, tools/phase6/final_evidence_check.py]
  modified: [backend/internal/worker/rule_worker.go, backend/internal/worker/outbox.go, backend/internal/worker/port_worker.go, backend/internal/service/port_operation.go, backend/internal/service/tracer.go, agent/regional_control_test.go, agent/control.go, agent/renewal.go, agent/renewal_test.go, agent/store/control_status.go, agent/store/control_status_test.go, hub/runtime_server.go, tools/download_release.py, tools/phase6/check.sh, tools/phase6/final_check.sh, tools/phase6/final_container_check.sh, .github/workflows/phase6.yml]
key-decisions:
  - "1000-rule qualification uses real PG and production reconciliation with a deterministic node adapter; actual mTLS/Agent/Engine/fault paths run separately."
  - "All operation-event writers lock tenant before business rows; task-only lease commits before waiting and exact durable lease is rechecked."
  - "No failed, partial, canceled, external-not-run, dirty or different-source result is paired acceptance."
requirements-completed: [BE-006]
completed: 2026-11-09
status: complete
---

# 06-11 最终证据与有界对账

相同实现提交 `65a5335894732b7d632e870acc3d8283be224cd3` 的本机Linux arm64 Docker与Ubuntu24.04原生amd64/arm64 Actions均完成完整后端、Agent/Hub/contracts组件和Engine普通/race、故障注入、双生成、精确批准契约、许可、六项交叉构建及真实Cloudflare CRUD。Actions run42572674719。三个producer均clean、无测试skip/FAIL/race；递归原始文件hash与语义门禁复核通过。

| Producer | Backend normal/race | Agent/Hub/contracts normal/race | Engine normal/race | 1000规则 normal/race | Cloudflare CRUD |
| --- | --- | --- | --- | --- | --- |
| docker | 502 / 502 | 657 / 657 | 249 / 249 | 8.567 / 13.649 秒 | PASS |
| actions-amd64 | 502 / 502 | 657 / 657 | 249 / 249 | 23.835 / 32.862 秒 | PASS |
| actions-arm64 | 502 / 502 | 657 / 657 | 249 / 249 | 29.746 / 42.092 秒 | PASS |

各host的1000/2000记录完整收敛，独立计时严格<=60000ms。最终配对通过check.sh与evidence_manifest.sh两入口；原始日志/JSON保存在 `artifacts/phase6/06-11/final-65a53358/`，跨域35条truth、29条keylink和前序18文件91测试逐项核验在verification-review/current-runtime-cross-check.json。

## 实现与故障修复

483f84e0引入每轮最多32项的就绪任务排空，遇到无工作才等待。4420e625/30653e9b统一规则授权锁序并降低全局恢复扫描频率。最终f54c8017复核到domain-event/audit tenant外键也参与锁等待，control、query/abort、port-release和aggregateprojection统一tenant→task顺序，先提交task-onlylease再复核token/state/deadline；投影tenant→group→allocation→task。Controlexpiry发现扫描回滚后，在逐租户事务内原子写taskunknown+operationevent，失败可重试。释放原artifact不重新申请激活，仍须实际authenticatedFULLproof才释放占用。

f466bfab限制每个既有retry turn最多4条原不可变续资请求，并按连接局部游标轮转未响应项，为串行心跳/回执留出处理机会；不改原3秒watchdog、20秒收敛或禁止隐藏重连断言。旧源码RED9vs4，native/Linux重复3race67.255/79.891s通过。

65a53358将Hub状态观察改为固定SQLite快照后再采样可信时间，避免并发新心跳使观察结果误报blocked；原future-heartbeat/clock/authority检查不放宽。确定性preclockRED和native/Linux回归、两项实际恢复10race171.349s通过。生成器下载只尝试原官方资产与精确官方API入口，仍核对原锁定摘要/版本。

a2097430将regionalfixture准备、5秒runtimefailover和账务证明分开计时，运行门槛不变，缺失heartbeat必须sql.ErrNoRows。验收脚本显式传播secretguard失败、转交root0600evidenceowner、宿主Ruby预检、私有policy绝对路径预检，并以整数纳秒兼容GoRFC3339Nano。

## 验证与偏差

六旧路径确定性RED55P03、八路径及expiryeventfaultGREEN；Linuxrace重复3的真实三进程/socket/proof/rollback49.941s，native22.715s；完整最终结果见表。原估算两任务被拆为多个原子修复，范围未缩减。所有OOM、网络/端口、Gitalternate、Ruby、owner、policy、timestamp、真实40P01、fixturecontext失败和被替代轮次在EXECUTION-HISTORY.md及原始目录保留。

此有限模型不代表1000实际Engine或200节点全规模；Agent/Hub/contracts组件测试不等于root所有package。标准Actions宿主是Ubuntu24.04，容器使用锁定Go镜像；不声称专用硬件性能、物理断电、完整形式证明或24小时稳定性。历史Phase3VLinkreviewhash债务、Phase4D-04/性能、Phase7中文前端/TypeScript生成/备份WAL恢复、Phase8全规模均保留。Cloudflare权限证据是已审查policyattestation，不冒称在线独立权限审计。
