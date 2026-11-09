# 06-07 REST / SSE 覆盖边界

BE-005 的业务边界是明确tenant作用域的REST、PG当前身份、精确操作查询和可重放SSE。候选OpenAPI3.0.0的41paths/45operations与Gin实际注册路由一一对应；公共响应采用安全字段投影。完整HTTP集成样例对候选schema检查额外字段、required、类型、枚举与引用。唯一OpenAPI源及后端副本必须逐字相等。

| 范围 | 实际验证与证据目录 |
| --- | --- |
| opaque access / rotating refresh / reuse / logout / current PG role | task1-tokens；task1-session-provision；auth/httpapi集成 |
| tenant/principal/collection cursor、排序和当前可见性 | task1-resource-lists；task1-public-contract |
| 强If-Match、精确operation、不可变修订与审计回滚 | task1-node-transition；task1-package-catalog；task1-package-http；task1-group-members |
| 全租户平台组依赖重编译 | task1-group-revalidation；原权限、修订及占用不因排队释放 |
| 端口prepare/commit/release/abort及原Node证明inventory | task1-port-operations；task1-port-http；task1-port-actions |
| serving与actor绑定DNS幂等快照和生产消费命令 | task1-serving-http；固定provider端点，unknown停放 |
| 既有恢复业务阶段与显式有界推进 | task1-recovery-http；真实SQLite FULL fence/root回执，不能新建批准或释放原债务 |
| 同事务事件、提交序号、PG重放、retention gap/resync、签名稳定ID | task2-events；SSE当前角色重检、慢客户端和资源边界 |
| 精确契约、语义负向检查、源码与构建元数据 | task1-public-contract；task2-unified-acceptance |

上述目录均位于 `artifacts/phase6/06-07/`。开发日志与干净源码日志分别保留，失败不改写。恢复并发用例发现的真实40P01锁反序，已修复为一致tenant→task→operation顺序；最初失败与后续完整native/Linux race同时保留。

`tools/phase6/rest_container_check.sh` 是06-07的有界干净源码producer；原 `check.sh --through-plan 06-07` 尚未实现，不能作为通过证据。新producer要求精确SHA/干净源、PG迁移、两次sqlc、完整backend normal/race及六项Linux双架构构建。host侧检查OpenAPI/lint-negative/依赖许可和原始Go JSON。`rest_evidence_check.py` 拒绝错SHA、脏源、skip、缺关键测试、生成数量或构建身份异常。

公开API3.0.0破坏性变化以及数据库2.4.0候选需要精确CR-009批准。此文不批准或冻结。Phase7消费约定见 `docs/operations/public-api.md`、`recovery-advance.md` 与 `dns-worker.md`。创建新恢复批准、DNS凭据登记仍是受信任运维流程；不增加公网身份自选或原始控制证明输入。

本机Docker实际执行架构和镜像见原始host/Compose日志。交叉编译不等于目标CPU执行；普通Linux开发/fixture测试不是Ubuntu24.04资格、性能、完整模型、物理断电或外部Cloudflare真实CRUD验收。最终同SHA GitHub Actions、外部依赖和跨域验收保持06-11，不以当前单元代替。
