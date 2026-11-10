---
phase: 06-m4-m4-backend
reviewed: 2026-11-09T07:06:16.325092+00:00
depth: standard
review_source: 65a5335894732b7d632e870acc3d8283be224cd3
review_execution: inline
files_reviewed: 44
files_reviewed_list:
  - agent/store/control_status.go
  - agent/store/control_status_test.go
  - hub/runtime_server.go
  - tools/download_release.py
  - tools/phase5/install_generators.sh
  - tools/phase6/install_sqlc.sh
  - tools/phase6/test_release_download_check.py
  - agent/control.go
  - agent/renewal.go
  - agent/renewal_test.go
  - backend/internal/worker/outbox.go
  - backend/internal/worker/port_worker.go
  - backend/internal/service/port_operation.go
  - backend/internal/service/tracer.go
  - backend/internal/service/worker_event_lock_order_test.go
  - agent/regional_control_test.go
  - backend/internal/policy/policy.go
  - backend/internal/repository/tenant_scope.go
  - backend/internal/repository/token_repository.go
  - backend/internal/auth/tokens.go
  - backend/internal/httpapi/router.go
  - backend/internal/httpapi/sse.go
  - backend/internal/httpapi/cursor.go
  - backend/internal/control/credential_crypto.go
  - backend/internal/control/credential_store.go
  - backend/internal/compiler/compiler.go
  - backend/internal/service/rule.go
  - backend/internal/service/entitlement.go
  - backend/internal/service/lease.go
  - backend/internal/service/hub_credit_settlement.go
  - backend/internal/service/runtime_funding_scheduler.go
  - backend/internal/worker/rule_worker.go
  - backend/internal/service/orchestration.go
  - backend/internal/service/rule_worker_lock_order_test.go
  - backend/internal/worker/rule_reconciliation.go
  - backend/internal/worker/rule_reconciliation_test.go
  - backend/internal/reconciliation/report.go
  - backend/internal/service/reconciliation_1000_test.go
  - tools/phase6/final_check.sh
  - tools/phase6/final_evidence_check.py
  - tools/phase6/test_final_evidence_check.py
  - tools/phase6/final_container_check.sh
  - tools/phase6/dependency_lock_check.sh
  - tools/phase6/rest_container_check.sh
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 06 代码审查

## Narrative Findings (AI reviewer)

当前44个显式文件范围内没有未解决的缺陷。按风险选择认证、租户scope/审计、加密凭据、编译输入、份额续租、原来源结算、资金接续、worker与验收producer；这是同一执行者内联审查，不是独立外部审计，也不声称逐行审查全部历史文件。生成代码由两次生成比较承担。

审查发现并修复 WR-06-01：`final_check.sh` 中处于 `&&/||` 条件的subshell不会按隐式errexit中断；凭据扫描失败后可能执行 `exit 0`。提交3fb8e25a显式传播扫描和环境加载失败。直接提取当前脚本guard、放入相同条件语义，合成泄漏exit1、脱敏exit0；证据 `artifacts/phase6/06-11/producer-secret-guard/negative.json`。20组工程语义负向测试通过。

核对的关键关系：公开请求不自选actor/role；PG查询复核当前身份；refresh family加锁并保留reuse撤销；cursor区分principal/tenant/collection且HMAC域分离；SSE重验权限并有并发/时长/写deadline；业务、audit、task和Outbox同事务；complete seal/child finals才结清原parent；续资不能以超时或耗尽替代final。bounded32 drain保留原Once的lease/receipt身份并在无可执行工作时等待。

## 验证限制

最终同SHA Docker/Actions仍在运行，其结果以06-VERIFICATION为准。既有Phase3 VLink review hash差异是历史治理债务，未授予新批准；Phase4专用性能、Phase7备份和UI、Phase8全规模/24h不在本次代码审查结论内。

追加复核WR-06-02：Linux容器root-owned0600报告阻止runner汇总/上传。d852c99d在无论测试成功/失败时把/evidence owner交回宿主UID/GID，保留mode与原退出码；真实Linux volume独立uid1001验证denied→readable、mode0600、exit23保持。macOS bind-mount因UID映射不能复现拒绝，保留该初次断言失败事实。

追加复核WR-06-03：固定Go镜像无Ruby，最终契约lint无法执行。49b64e03将既有Ruby结构lint、精确批准与许可检查放到宿主预检，Linux sqlc/protobuf双生成仍在container。开发预检20组语义测试、两次sqlc/许可及99产物精确冻结均通过；完整新同SHA三端验收仍执行中。完整后端套件显式20m预算；1000规则报告严格60s，Actions wall90m。

追加复核WR-06-04：原49b两架构Actions的真实tracer均发生40P01，1000规则race在60秒仅完成973/971条。4420e625与30653e9b将Prepare/Commit task-only lease独立提交，再按tenant→task复核当前token/state/expiry/authority，receipt/deployment按tenant→head；query/abort保留无新增授权的单租约事务。确定性PG测试RED55P03、修复GREEN且superseded不发送；真实三进程与unknown/rollback测试通过。Run恢复扫描启动时及每秒执行，Once仍每次显式恢复；60秒预算不变。新完整三端结果仍独立门控。

追加复核WR-06-05：Python3.9不能解析Go输出8位小数RFC3339Nano。改为严格UTC格式及整数纳秒，保留全部精度，跨秒与0..9位、非法格式测试通过；原16.302/20.631秒报告可解析。私有Cloudflare相对policy路径前置检查为绝对可读路径，原始失败与只重试真实CRUD的provenance保留，不改写为首次通过。

追加复核WR-06-06：30653e9b本机root race的故障切换已完成，但账务证明复用了包括SQLite迁移和TLS准备的5秒上下文。a2097430分离准备30s、运行5s和证明5s，保留原dial/watchdog/failover/retry及5秒运行断言；心跳缺失必须为sql.ErrNoRows，超时不能当作缺失。原生5次与Linux3次race通过，原始失败保留，完整新三端待结果。

追加复核WR-06-07：30653e9b Actions amd64原始日志确认port-operation40P01，arm64虽完整通过但无三端配对。事件插入的tenant外键也可形成task→tenant反序，故query/abort单事务优化撤回。f54c8017统一control/rule/port-release tenant→task复核，projection tenant→group→allocation→task；lease token/state/deadline逐次重验。释放只重构原artifact用于stop，不能签发激活。过期扫描临时结果回滚，再逐项原子提交taskunknown+operationevent，事件失败保留running可重试。六路径旧代码重复3RED55P03、修复八路径+expiryfault重复3GREEN，真实三进程/socket/proof/rollback Linuxrace49.941s与native22.715s通过。最终完整三端仍门控。

追加复核WR-06-08：f54c8017 Actions amd64实际33规则续资虽安装完成，但整页128项发送占用串行控制循环，3秒watchdog触发一次隐藏重连。f466bfab限制每轮4条原journal请求，并按连接局部ID游标轮转；未响应可重试，重连从原journal起点开始。游标只观察发送进度，不能生成权利或改变原ID/digest；不放宽watchdog、20秒收敛或Attempts==1。旧源码新测试RED首轮9vs4；native重复3race67.255s、Linux79.891s通过，含33规则真实mTLS、旧会话拒绝、丢result重放。最终完整三端仍门控。

追加复核WR-06-09：f466bfab原生amd64Actions恢复续资发生额外一次未就绪连接；诊断overlay复现心跳在状态读前提交，比观察时刻晚5.675微秒，导致blocked。65a53358让RuntimeServer固定SQLite读快照后采样可信观察时间；未知/未来时刻仍拒绝，授权签发独立校验，原截止不延长。确定性旧时序RED、跨连接GREEN；native3race3.470s、Linux两项实际恢复10race171.349s全通过，不放宽Attempts2。

追加复核WR-06-10：官方GitHub资产连续504阻断sqlc/Protobuf生成；下载helper仅按原URL、同资产download参数和精确tag/name的官方API重试，每次仍必须通过原SHA256、version与license。下载失败/部分字节/恶意metadata来源拒绝。26工程组、Linux真实protoc36.2及两次33sqlc/native依赖预检通过；完整新三端独立门控。

最终运行确认：实现65a5335894732b7d632e870acc3d8283be224cd3三端normal/race、fault、external与pair均通过。未解决Critical0/Warning0/Info0；上文pending叙述保留其发现时上下文，最终结论以本段和06-VERIFICATION为准。
