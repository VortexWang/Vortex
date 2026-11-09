# API Coverage — VORTEX Phase 7

> Full coverage by default. Opt-outs are explicit, reasoned decisions. 范围为既有Cloudflare DNS记录协调集成与本阶段浏览器消费的VORTEX公共REST；不是宣布全Cloudflare产品API属于本项目。实际detector对中文计划scope返回detected:false/exit1（不是skipped），这里仍显式记录现有集成的覆盖，避免语言触发词漏检造成隐形缺口。

| capability | decision | reason |
| --- | --- | --- |
| Cloudflare DNS Records: list / read | INTEGRATE | 07-54/07-55/07-56 固定可信provider、scope内desired/serving/observed读取与受控真实zone证据 |
| Cloudflare DNS Records: create | INTEGRATE | 07-54/07-55/07-56 真实worker幂等创建测试记录，非客户端endpoint |
| Cloudflare DNS Records: update PUT / PATCH | INTEGRATE | 07-54/07-55/07-56 版本/内容核对和失败重试，安全投影 |
| Cloudflare DNS Records: delete | INTEGRATE | 07-54/07-55/07-56 仅属于VORTEX配置或本次唯一测试记录，不误删外部记录 |
| Cloudflare DNS Records: pagination / bounded filtering | INTEGRATE | 07-54/07-55/07-56 查询集合与scope有界，确保完整实际集合而非单页成功 |
| Cloudflare DNS Records: rate limit / provider errors / unknown / retry | INTEGRATE | 07-54/07-55/07-56/35 429/超时/丢响应保守重试，保留真实失败 |
| Cloudflare DNS Records: batch endpoint | OPT-OUT | 本阶段定义为VORTEX serving集合幂等逐项协调，不交付Cloudflare通用批量DNS管理客户端；不会省略集合一致性 |
| Cloudflare DNS Records: BIND import / export | OPT-OUT | VORTEX权威期望配置来自PG，不导入外部DNS区文件或把外部导出作业务权威 |
| Cloudflare DNS Records: scan / review discovered records | OPT-OUT | 无DNS托管迁入/自动发现业务要求，不能把扫描结果自动成为VORTEX期望成员 |
| Cloudflare DNS Records: per-record nonrouting metadata/comment tags | OPT-OUT | 当前仅VORTEX记录身份/安全生命周期所需元数据由既有adapter管理，不做通用CF元数据编辑器 |
| Cloudflare Zone DNS settings / DNSSEC / zone transfers | OPT-OUT | 区域全局运维不属于VORTEX serving记录协调；不扩大测试凭据到区级安全配置 |
| Cloudflare analytics / DNS firewall / account / billing / zone CRUD | OPT-OUT | VORTEX只协调授权zone内DNS记录，不成为Cloudflare账户运营平台 |
| Cloudflare token validation / account-wide permission administration | OPT-OUT | 权限范围沿受审私有policy attestation及受限测试zone核验，不冒称token有效性等于独立在线权限审计 |
| Other backup/object-storage/KMS external APIs | OPT-OUT | D-07-16只锁定adapter seam，未指定第三方产品；默认私有部署方案07-74/28真实实现，不虚构第三方集成 |
| VORTEX public operationId: refreshSession | INTEGRATE | 07-08/07-09/07-10/07-11/07-12真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: logoutSession | INTEGRATE | 07-08/07-09/07-10/07-11/07-12真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getPrincipal | INTEGRATE | 07-08/07-09/07-10/07-11/07-12真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getTenant | INTEGRATE | 07-17/07-18/07-19真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listTenants | INTEGRATE | 07-17/07-18/07-19真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getNode | INTEGRATE | 07-20/07-21/07-22真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listNodes | INTEGRATE | 07-20/07-21/07-22真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: transitionNode | INTEGRATE | 07-20/07-21/07-22真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getNodeGroup | INTEGRATE | 07-23/07-24/07-25真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getGroupMembers | INTEGRATE | 07-23/07-24/07-25真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listNodeGroups | INTEGRATE | 07-23/07-24/07-25真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: replaceGroupMembers | INTEGRATE | 07-23/07-24/07-25真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getRule | INTEGRATE | 07-31/07-32/07-33/07-34/07-35真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listRules | INTEGRATE | 07-31/07-32/07-33/07-34/07-35真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: reviseRule | INTEGRATE | 07-31/07-32/07-33/07-34/07-35真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getPackage | INTEGRATE | 07-46/07-47真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getSubscription | INTEGRATE | 07-46/07-47真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listPackages | INTEGRATE | 07-46/07-47真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listSubscriptions | INTEGRATE | 07-46/07-47真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: putPackageCatalog | INTEGRATE | 07-46/07-47真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: previewSubscription | INTEGRATE | 07-46/07-47真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: confirmSubscription | INTEGRATE | 07-46/07-47真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getSourceLedger | INTEGRATE | 07-51/07-52/07-53真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getTenantLimits | INTEGRATE | 07-51/07-52/07-53真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getBillingExplanation | INTEGRATE | 07-51/07-52/07-53真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listSources | INTEGRATE | 07-51/07-52/07-53真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listRestrictions | INTEGRATE | 07-51/07-52/07-53真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: redeemEntitlement | INTEGRATE | 07-48/07-49/07-50真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: previewEntitlementRedemptions | INTEGRATE | 07-48/07-49/07-50真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: confirmEntitlementRedemptions | INTEGRATE | 07-48/07-49/07-50真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listPortAllocations | INTEGRATE | 07-29/07-30真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getPortAllocation | INTEGRATE | 07-29/07-30真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: preparePortReservation | INTEGRATE | 07-29/07-30真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: commitPortAllocation | INTEGRATE | 07-29/07-30真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: releasePortAllocation | INTEGRATE | 07-29/07-30真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: abortPortAllocation | INTEGRATE | 07-29/07-30真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getNodeRecovery | INTEGRATE | 07-82/07-83真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getHubSameStoreRecovery | INTEGRATE | 07-82/07-83真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getHubColdRecovery | INTEGRATE | 07-82/07-83真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: requestHubRecoveryAdvance | INTEGRATE | 07-82/07-83真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getServingAddresses | INTEGRATE | 07-54/07-55/07-56真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: recomputeDNS | INTEGRATE | 07-54/07-55/07-56真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: listOperations | INTEGRATE | 07-31/07-32/07-33/07-34/07-35真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: getOperation | INTEGRATE | 07-31/07-32/07-33/07-34/07-35真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |
| VORTEX public operationId: streamEvents | INTEGRATE | 07-31/07-32/07-33/07-34/07-35真实业务使用/安全边界验证，07-01/07-02/07-03矩阵与07-84全量不漂移检查 |

当前公共45 operations完整保留；新增method/path/DTO由07-01/07-02/07-03具体候选与逐页矩阵精确列出、owner hash批准，矩阵每页每角色必须连接真实service/task/运行证据。无OPT-OUT用来省略27要求或32决策。Cloudflare全记录集合一致性、端口证据型释放与实际worker失败重试仍完整交付。

