# 06-04 执行证据：精确批准与本机验证通过

当前最终验证源为 `9db5da0eb1d8d5f20baa7802ca37007588d6cc35`。
用户已明确“批准 CR-006”，精确 JSON SHA256 仍为
`60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d`。
批准记录时间为 2026-11-07T20:45:23Z，DB/API2.1.0 已冻结；CR005 原历史保持。
同名新目录保存批准后重新运行的 clean clone 原始证据，未覆盖下文历史 pending 结果。

完整 check.sh --through-plan 06-04 --produce docker 通过：result=0、
source_clean=true、acceptance_complete=true（仅本机 producer）。
backend 全包普通/race service8.915s/15.177s、Agent/Engine/contracts 全包
普通/race、真实进程、sqlc 两次独立生成、OpenAPI lint/cmp、四条冻结、
wire/schema/map/SemVer、Linux双架构编译、真实Cloudflare CRUD均通过。
verification/ 保存完整入口日志、批准元数据/历史/规范/catalog/changelog
五种篡改反例及 manifest 反例；删到期测试、篡改校验和、缺 Actions 均拒绝。
最终同提交 Actions 配对仍属于 06-11；不把交叉编译当双架构原生运行。

## 以下为批准前历史证据


最新安全修复验证源为 `a9275d8660d5041cb9d7d4b7ab4e22c3095624ec`，同名目录
保存独立 clean clone 的完整新一轮证据。以下 d7e83d76 内容保留为前轮历史，
不能拿来证明 a9275d86 已验证。CR-006 规范候选字节和哈希保持原值。

复核发现：到期前节点 offline 或授权被撤销时，原 appendRuleRevision 的
激活 resolve 会使整笔到期事务回滚，固定队首租户也会阻塞其他租户。
`expiry-authority-red.json` 保留真实 PostgreSQL 两种失败及 RED 分类证据。
修复仅允许输入严格等于已存修订且 Enabled=false 的停止修订复用已存
segments/dependencies，仍持久化编译任务，不伪造 active、在线或停止确认。
其他 disabled 修改仍完整校验当前权限与节点。测试同时证明到期限制、来源
到期、七天保留、unknown occupation 和其他租户的生命周期正常推进。

新源本机 backend 全包普通/race 通过（service 13.965s / 19.098s）；clean
Linux Docker backend 全包普通/race 通过（8.998s / 15.116s）；完整
Agent/Engine/contracts 普通/race、sqlc 两次独立生成、amd64/arm64 Linux
编译、真实 Cloudflare CRUD（4.74s）和 host manifest 均通过。
删除新到期测试的反例被 manifest 拒绝，pending-owner 配对仍拒绝。
新 `result.json` 保持 result=0、source_clean=true、acceptance_complete=false。
仍无本轮 Actions 配对，也无契约批准或 completed SUMMARY/STATE 推进。

当前运行源：`d7e83d76b7ce6e0687ff66c6d6d60a4cef2b9523`。
该源通过独立 clean clone 执行（before/after status 均为空），不是把主工作区
未提交内容作为已验证源码。后续仅提交本证据和 checkpoint，不混称新的源码提交
已经运行过测试。

精确规范候选：`docs/changes/CR-006-packages-entitlements.json`，SHA-256
`60487b773b60f9b1bfee80811e49c17d43b910b7ff19c68025a33c9c5e83b28d`。
DB/API 2.1.0 仍是 candidate；CR-005 历史批准保持原字节。

| 项目 | 结果与位置 |
| --- | --- |
| 本机 backend 普通 / race，TZ=UTC | native/ 两日志；service 13.619s / 18.228s |
| clean clone Linux Docker backend 普通 / race | docker/tracer.log；service 8.825s / 16.923s |
| Agent/Engine/契约组件普通 / race | docker/rule-components.log；无 skip、失败或 race |
| Linux amd64 / arm64 构建 | docker/rule-builds.log；vcs.modified=false；不当作双架构原生运行 |
| sqlc 两次独立生成、锁定版本与许可 | docker/dependencies.log；与 committed source 完全一致 |
| OpenAPI lint、source/copy cmp | docker/openapi.log；16 个业务操作 |
| CR-005 历史 + 未变 wire/runtime + CR-006 精确哈希 | docker/rule-contract.log |
| Cloudflare 真实 CRUD | docker/cloudflare.log/json；4.36s，TTL60、DNS-only、精确名称清理 |
| 六条迁移 up/status | verification/goose-up-status.log；已审查 Goose v3.29.0，PG-only 临时 runner 源码与 build metadata 同存 |
| 计划三个任务与整体筛选命令 | verification/plan-verification.log；此日志生成于 a071c601，后续 UTC 修复由上面最终全包回归覆盖 |
| 门禁反例 | verification/gate-negative.json；待批准配对、删关键测试记录、日志校验和篡改均拒绝 |

`docker/result.json` 的 result=0、source_clean=true，Cloudflare 与 candidate
validation 均 passed。acceptance_complete=false：仅因契约待 owner 批准。
整个 check.sh 最终 exit=1 是明确 freeze gate，不伪装为总体验收通过。
单 Docker manifest 通过；Actions 本轮未运行，最终同提交配对仍属 06-11。

没有安装或伪称使用上游全驱动 Goose CLI。临时 PostgreSQL-only runner 仅链接
现有已审查的 Goose v3.29.0 与 pgx，运行相同 up/status API；未新增依赖或改写
旧迁移。seeded00005→00006 的权威数据保留测试包含在最终普通/race日志中。

`failed/` 保留两次真正失败的 clean-source尝试，不计 passed：ad00a4b4 因
regenerated schema 未提交而被哈希门禁拒绝；a071c601 在 Linux UTC 暴露首次兑换
与重放的时间位置对象不一致。后者经过实库 RED 分类，再以 UTC 统一修复。
raw task3-red.json 是前一执行器证据，未覆盖。

未实现的资金账本 / signer/path/lease producer 属于 06-05/06-06；HTTP 属于
06-07。没有用虚构资金或默认 epoch 打通部署。期望停止修订与实际停止确认
分别保留，未知占用和负债不被释放。详细边界见 docs/coverage/phase6/PACKAGES.md。
