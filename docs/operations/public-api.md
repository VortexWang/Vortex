# REST / SSE 接入边界（CR-009候选）

唯一 OpenAPI 源为 `contracts/openapi/v1/openapi.yaml`，采用JSON格式的有效YAML，便于无新增依赖地验证真实响应。`backend/openapi/v1/openapi.yaml` 逐字复制，禁止独立编辑。public API候选版本3.0.0与URL前缀 `/api/v1` 独立；本候选有破坏性变更，原2.3.0客户端需要更新路径、ETag、状态码及响应模型，不提供隐式旧路径兼容。

初始会话通过既有 `issue-session` 受信任本地命令，在独立身份核验后读取PG当前用户/平台角色并签发。不存在以公网 actor_id/tenant_id/role 自选身份的登录接口。短期opaque Bearer每次查询当前身份、角色、账号状态；`POST /api/v1/auth/refresh` 仅凭JSON内轮转refresh_token认证，重用会撤销整族。`POST /api/v1/auth/logout` 撤销当前会话族。数据库及审计不保存原始令牌。部署应使用独立TLS监听，不能将Agent/Hub mTLS控制通道当浏览器登录通道。

业务资源明确置于 `/api/v1/tenants/{tenant_id}`。平台身份必须显式选择租户，并对跨租户读取同事务审计；普通用户只能访问自己的租户。`GET /api/v1/me` 返回当前身份摘要，`GET /api/v1/tenants` 按当前权限分页。节点、组、规则、套餐、订阅、端口、来源、限制和操作使用显式安全字段，不能把数据库生成模型或控制payload直接序列化给客户端。

列表默认50、最多100条，按不可变排序登记时间及ID升序。历史资源的登记时间是migration9回填时刻，不冒充历史创建时间。签名cursor绑定主体、租户、集合及方向；每页重新核验当前可见性。最后一页省略next_cursor，items始终为数组。SSE和列表cursor不能互用。

需要CAS的写请求携带一个强引用的十进制 `If-Match: "7"`。创建规则/套餐可使用 `"0"`；节点、组、订阅及端口使用当前正版本。缺失为428，格式错误400，版本变化412，业务状态/幂等冲突409；统一错误字段为code/message/request_id，可选安全field_errors。JSON请求不接受未知字段，实际边界限制见OpenAPI与接口。

业务写入响应202和精确operation_id/Location，客户端查询对应操作和资源。套餐效果可能已在同一事务生效，编排仍待消费；端口commit成功只说明PG分配阶段，不能说明Engine已激活。release/abort、份额关闭、运行结清和恢复各自需要真实回执，超时/断连/unknown不释放占用。DNS任务由独立run-dns进程执行；恢复业务阶段/推进边界见同目录专用文档。创建新恢复批准、DNS凭据登记、在线支付不在本公网API内。

`GET /api/v1/tenants/{tenant_id}/events` 使用Bearer，不接受query token。事件由业务事务在PG提交时分配每租户序号，SSE帧含id/event/data；ID为绑定主体/租户的签名opaque cursor。Last-Event-ID从已提交历史续传；retention或序号缺口发送resync然后关闭，客户端通过REST重新读取状态。summary只含安全摘要；事件不是业务权威。连接最多5分钟，每15秒心跳，每秒重新检查会话，单实例最多64条流；慢客户端写入期限5秒。

Phase7依赖这些业务DTO、操作查询及resync约定。旧Node/Hub未知执行、恢复批准和凭据管理仍需受信任运维流程，前端不能生成boot/epoch、原始控制命令或成功证明来恢复业务。
