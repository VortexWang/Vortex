# CR-009：公共 REST / 认证 / 持久化 SSE 契约候选

状态：待 owner 明确批准。精确审查单元为同目录 `CR-009-rest-events.json`；规范清单含99个文件的SHA-256，绑定已批准CR-008基线。本文不创建approval、不冻结契约、不标记06-07完成。精确JSON SHA-256：`ed7ca2f0cf68f46d29984b7ddafbc13fc8de4ad171cd3378b0b9dda8e45bdfe0`；检查点见06-07-CHECKPOINT。

| 契约 | 版本 | 兼容性决定 |
| --- | --- | --- |
| public API | 2.3.0 →3.0.0 | breaking；以实现后的明确tenant路径、强CAS、错误/状态/DTO/分页/SSE语义替换此前待实现契约 |
| database | 2.3.0 →2.4.0 | additive；migration9增加hash-only会话、事务事件流、稳定公共排序、不可变端口聚合操作绑定 |
| control / runtime / authorization / VLink | 原冻结版本及字节 | migration1–8、既有codec、历史proposal/approval和治理记录保持原字节 |

唯一OpenAPI源 `contracts/openapi/v1/openapi.yaml` 使用JSON格式的有效YAML；后端逐字复制。41paths/45operations与Gin实际注册路由双向对齐，HTTP集成样例的真实响应检查required、额外字段、类型、枚举与引用。没有公网actor/role自选登录；初始签发使用独立身份核验后的本地issue-session命令，refresh仅有一个明确的轮转body token认证例外，重用撤销整个族。

必须更新旧客户端的项目：无租户前缀的limits、billing、source ledger、catalog路径被明确tenant路径替代；If-Match改为一个强引用的规范十进制；参数错误400、缺条件428、陈旧412、状态冲突409；权益兑换202和精确operation_id；DNS幂等键在header；最后一页省略next_cursor；safe DTO与SSE帧采用实际实现字段。URL仍为/api/v1，契约SemVer与URL前缀独立，不提供隐式旧路径别名。

业务写入只提交可追踪期望或已提交业务阶段。端口commit成功不是Engine激活；release/abort要完整原Node库存的实际回执。DNS外部调用由独立run-dns消费，未知结果停放；相同key重放原快照。平台恢复GET只表示已持久化阶段，推进仅引用既有批准，并等待真实manifest/Node FULL fence/root；PG准入不代表Hub本地完成或原债务可释放。新恢复批准、boot/epoch/backend/原始证明输入、DNS凭据登记和在线支付不在本公网边界。

SSE由PG业务事务在提交时按租户分配序号，ID签名绑定当前主体和租户；Last-Event-ID重放持久历史，缺口发resync并关闭，由REST恢复权威状态。会话定期重检，连接数量/寿命/心跳/写入时限有界。Valkey不保存可靠业务权威。

覆盖、运行命令及Phase7依赖见 `docs/coverage/phase6/REST-SSE.md`、`docs/operations/public-api.md`、`recovery-advance.md`、`dns-worker.md`。完整开发native/Linux backend race已通过；干净源码`97966a3a`的完整backend normal/race各482测试及子场景通过（service227.988/383.572秒），两次33输出sqlc和六项Linux双架构构建通过；元数据与99规范文件核对一致。独立Docker启动曾因本机端口占用失败且未开始测试，已保留日志并改用空闲端口。恢复测试曾检出真实40P01锁反序，已修复并保留原失败与后续完整通过结果。

本候选不代表最终同SHA GitHub Actions配对、Ubuntu24.04硬件资格、性能、完整模型、物理断电或外部Cloudflare真实CRUD验收；这些证据仍按06-11/后续验收范围处理，不把fixture或交叉编译当成原生目标运行。
