# 06-07 public contract candidate development evidence

基线b55efab2，public API3.0.0 breaking / database2.4.0 additive候选，尚无CR-009批准。单一OpenAPI源使用JSON格式的有效YAML，后端副本逐字相同；无新增运行依赖。Gin实际注册路由与41paths/45operations一一对应。既有HTTP集成请求的真实JSON响应由通用schema断言核对required、额外字段、类型、枚举、引用、数组及数字界限；不存在只比较手写DTO样例的替代验收。

完整HTTP竞态native8.978s、Linux7.700s通过（Linux另含auth/repository/policy）。Ruby lint与5个语义negative测试通过：拒绝未认证普通路由、refresh例外扩散、内部boot/target_revision、悬空引用、开放对象、bare CAS及缺错误响应。历史API2.3.0在新validator下也曾验证通过；不把它当实现符合旧契约。最终check-map/SemVer均通过。

业务代码完整native/Linux backend race与双架构构建继承上一单元task1-recovery-http，对应源b55efab2之前同一生产代码；本单元修改契约与验证测试，未修改生产Go实现。迁移9的fresh/upgrade/审计/并发证据分见本计划各原子单元。干净源码统一检查与精确CR-009 inventory随后补充。历史CR-008proposal/approval和治理记录不改写。

最终精确候选checker与六类负向候选检查见exact-candidate-corrected.log / exact-candidate-negatives.log、task2-unified-acceptance/final-semantic-gates.log。exact-candidate.log首次Git中文quoted路径解析失败，已改NUL路径；该工具错误不影响规范内容。完整干净源证据已完成，见相邻task2-unified-acceptance。
