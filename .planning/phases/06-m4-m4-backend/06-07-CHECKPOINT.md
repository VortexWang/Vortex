# 06-07 CR-009 owner审批检查点

状态：实现与候选验收完成，等待精确CR-009批准。Phase6仍9/11，06-07未完成，06-11未开始最终验收；无06-07 SUMMARY。

审查单元：`docs/changes/CR-009-rest-events.json`，SHA-256 `ed7ca2f0cf68f46d29984b7ddafbc13fc8de4ad171cd3378b0b9dda8e45bdfe0`，99个规范/迁移/SQL/生成文件。干净测试源 `97966a3a70bff79afd43078ceadf211fe877a6d5` 与全部99文件一致。

- Public API2.3.0→3.0.0，breaking：明确tenant路径、强引用If-Match、实际错误/状态、精确operation、安全DTO、header幂等、分页与SSE帧。
- Database2.3.0→2.4.0，additive migration9：hash-only会话、可靠事务事件、公共稳定排序、端口聚合绑定。
- Control1.6.0/runtime1.2.0/authorization1.2.0/VLink1.0.0及migration1–8原字节不变；所有历史CR与治理记录不变。
- 45个已注册业务操作双向对齐，真实HTTP响应schema、当前权限/隔离、并发CAS/幂等/审计回滚、SSE持久重放/resync和实际Node FULL恢复场景通过。
- 干净Linuxarm64 normal/race各482测试及子场景，service227.988/383.572s；两次33输出sqlc、六项构建、依赖/许可、证据与候选负向门禁通过。原始证据：artifacts/phase6/06-07/task2-unified-acceptance/。

端口commit只表示PG阶段，恢复PGadmission不是Hub本地完成；旧财务/端口未知占用不因任务成功/expiry/断连释放。新恢复批准、rawboot/epoch/backend/proof、凭据登记、在线支付不开放公网。Phase7依赖和运维边界见docs/operations/public-api.md及对应DNS/recovery文档。

批准后：以精确JSON创建独立CR-009-APPROVAL，更新候选catalog为frozen并追加治理记录，复验exact freeze/历史保护，才完成06-07；随后执行06-11通用producer/最终同SHA Docker与Actions/跨域验收。当前只审查规范，不把本机Docker等同nativeamd64、性能或外部Cloudflare验收。

批准来源说明：docs/governance/contract-review.yaml:70明确“Any normative change requires owner approval”。本次改变已冻结规范且publicAPI含breaking变化，CR-008授权不自动延伸。gsd-execute-phase SKILL.md要求“Preserve all workflow gates”；这是保留项目既有审批检查点，不是该skill另行发明审批。需owner明确回复“批准 CR-009”。
