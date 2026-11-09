# CR-006：套餐、权益码与限制生命周期候选

状态：**待 owner 明确批准**。精确审查单元为同目录
`CR-006-packages-entitlements.json`，SHA-256：
`60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d`。
JSON 列出每份规范文件的当前与基线哈希。基线为已批准 CR-005 冻结提交
`fc63d57406f9f0e3eebe99ad276f95b004add710`；历史批准文件不改写。

| 契约 | 变更 | 兼容性 |
| --- | --- | --- |
| vortex.database | 2.0.0 → 2.1.0 candidate | 新增 00006；旧迁移与数据不改写 |
| vortex.public-api | 2.0.0 → 2.1.0 candidate | 新增套餐/兑换业务操作；已有业务操作不改写 |
| control / runtime / authorization / VLink | 版本与字节保持基线 | 不扩大 CR-005 或 Phase 5 批准 |

新增不可变目录版本、授予快照、主订阅、独立流量来源和计费快照，持久化
兑换、未来套餐变更与来源到期。可变目录不回写历史。UTC 月锚点从原始时间
推导；月度来源仅在到期边界签发，套餐到期产生限制并追加禁用规则修订。
来源到期不释放任何未证明的占用或未知负债。

跨套餐码的预览绑定码哈希、actor、package version 和完整影响摘要。确认
事务消费一次码并同时持久化 redemption、快照、未来变更、task/outbox/audit；
码的天数和额度是授予条款，能力及重置策略来自指定版本。future effective_at
不会提前激活。worker 重查操作者与影响，过时变更失败且无部分变更。

**需要审查的业务语义：码在确认成功时消费；未来变更被撤权或输入变化拒绝
时不自动退款/恢复次数。** 此时既有订阅保持原事实，消耗记录可审计，不能
静默重复授予。相同幂等键重放原结果，改码/actor/输入必须冲突。

OpenAPI 包含目录编辑、订阅存储条款、套餐/码的 preview 与 confirm；HTTP
实现仍属 06-07。资金账本、signer/path/lease producer 仍属 06-05/06-06，
没有虚构资金或 epoch。在线支付、自动续费、双架构原生压测均未声称交付。

验证范围与真实测试见 `docs/coverage/phase6/PACKAGES.md`。本机与 Docker
结果、同提交身份、外部 Cloudflare setup 与最终 Actions 配对单独记录；
候选验证通过不代表 owner 冻结批准，pending 时总体验收保持未通过。
