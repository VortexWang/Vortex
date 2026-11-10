# Phase 7 — Spec-less Edge Disposition

输入为原始edge-probe.json与requirements.json；无SPEC。保留53个surfaced items：51 explicit truths、0 backstop、2 unclassified unresolved。没有auto-dismiss；每个explicit谓词原样进入指定PLAN must_haves.truths。若后续需要backstop只能用{statement, verification: backstop}结构，不用括号散文。unclassified不能转backstop自动解决；保留给人工taxonomy裁决而不缩减源文行为。

| ID | Req | Category | Status | Verification | Plan | Observable resolution / flagged assumption |
| --- | --- | --- | --- | --- | --- | --- |
| E-07-01 | FE-001 | empty | resolved | explicit | 07-04/07-05/07-06/07-07 | 中文页面空/单项/null以空态或未知表达，缺失数量不填零。 |
| E-07-02 | FE-001 | encoding | resolved | explicit | 07-04/07-05/07-06/07-07 | 中文/emoji/组合字符按Unicode code point校验显示长度，UTF-8非法输入拒绝；金额和版本不按本地化文本计算。 |
| E-07-03 | FE-002 | empty | resolved | explicit | 07-17/07-18/07-19 | 空列表仍有当前授权的列表创建按钮，无常驻创建表单；一项时结构保持。 |
| E-07-04 | FE-002 | encoding | resolved | explicit | 07-17/07-18/07-19 | 名称Unicode文本不改变创建入口/路由语义，API/UI采用相同code point长度限制。 |
| E-07-05 | FE-003 | empty | resolved | explicit | 07-31/07-32/07-33/07-34/07-35 | 缺入口地址显示—且保留名称第一、入口第二列，空列表列标题仍存在。 |
| E-07-06 | FE-003 | encoding | resolved | explicit | 07-31/07-32/07-33/07-34/07-35 | 中文/emoji名称与IPv6入口不改变列序，完整值可访问且安全转义。 |
| E-07-07 | FE-004 | empty | resolved | explicit | 07-31/07-32/07-33/07-34/07-35 | 缺节点证据显示unknown/部分与原确认时间，不推断applied/可用/停止。 |
| E-07-08 | FE-004 | encoding | resolved | explicit | 07-31/07-32/07-33/07-34/07-35 | 版本按批准opaque字符串逐字比较，状态/限制机读码canonical，中文只用于展示。 |
| E-07-09 | FE-005 | empty | resolved | explicit | 07-36/07-37/07-38 | 无有效候选/段许可时禁提交并说明原因，入口或出口空值拒绝，不选择首个未授权组。 |
| E-07-10 | FE-005 | encoding | resolved | explicit | 07-36/07-37/07-38 | 组/许可ID作为opaque canonical值传输，中文名不作为授权身份；畸形ID拒绝。 |
| E-07-11 | FE-006 | empty | resolved | explicit | 07-41/07-42/07-43 | 未测/null/失败延迟显示—，stale保留真实确认值和时间，不填0或估算。 |
| E-07-12 | FE-006 | encoding | resolved | explicit | 07-41/07-42/07-43 | 延迟原始值为批准整数单位并范围校验，中文/本地数字只在展示层，不改变实测值。 |
| E-07-13 | FE-007 | empty | resolved | explicit | 07-39/07-40 | 会话水位/影响未知明确unknown，危险提交阻断，不把null损失或会话数当0。 |
| E-07-14 | FE-007 | encoding | resolved | explicit | 07-39/07-40 | 预览/版本/水位ID按opaque canonical值绑定，精确会话数不通过浮点或本地化文本重算。 |
| E-07-15 | FE-008 | empty | resolved | explicit | 07-61/07-62/07-63 | 空/未知错误码有中文安全兜底原因、建议和request_id，不暴露内部原文或空弹窗。 |
| E-07-16 | FE-008 | encoding | resolved | explicit | 07-61/07-62/07-63 | 错误码ASCII canonical映射中文，字段/段引用安全转义，emoji输入不破坏错误定位。 |
| E-07-17 | FE-009 | empty | resolved | explicit | 07-29/07-30 | 损失未知时危险提交阻断；只有权威完整预览确认无损失才显示无损失，空弹窗不代确认。 |
| E-07-18 | FE-009 | encoding | resolved | explicit | 07-29/07-30 | 损失金额/数量使用后端十进制字符串，不因locale/Unicode数字被重新解释。 |
| E-07-19 | FE-010 | empty | resolved | explicit | 07-59/07-60 | 空通知列表显示空态；未读统计缺失显示未知，不能凭空清零，单条关键通知始终可见。 |
| E-07-20 | FE-010 | encoding | resolved | explicit | 07-59/07-60 | 事件/周期/对象身份用后端opaque ID，中文通知文本不参与归并键或恢复判定。 |
| E-07-21 | FE-011 | empty | resolved | explicit | 07-51/07-52/07-53 | 无真实样本/统计水位则时效未通过，界面保留确认值标stale，不把空采样算P95=0。 |
| E-07-22 | FE-011 | encoding | resolved | explicit | 07-51/07-52/07-53 | UTC/RFC3339时间和整数纳秒计算保持精确，本地日期格式只展示，时钟误差写证据。 |
| E-07-23 | FE-012 | empty | resolved | explicit | 07-44/07-45 | 区域列表/阶段缺失不推断正常；已有降级持续到权威阶段恢复，不能因空响应撤横幅。 |
| E-07-24 | FE-012 | encoding | resolved | explicit | 07-44/07-45 | 区域ID和机读模式canonical，翻译/中文区域名称不改变规则禁写范围。 |
| E-07-25 | SEC-001 | empty | resolved | explicit | 07-67/07-68/07-69 | 无证书/空链/缺身份拒绝握手与运行，注册凭据不能补运行身份缺口。 |
| E-07-26 | SEC-001 | encoding | resolved | explicit | 07-67/07-68/07-69 | X509 DER和身份字节严格解析，node/role/boot不Unicode归一化；畸形证书拒绝。 |
| E-07-27 | SEC-002 | empty | resolved | explicit | 07-67/07-68/07-69 | 空/缺失吊销更新不是清空吊销集，仅完整可信版本可更新；缺状态不允许新连接绕过。 |
| E-07-28 | SEC-002 | encoding | resolved | explicit | 07-67/07-68/07-69 | 吊销对象与版本保持canonical身份字节，Unicode别名或畸形身份不能绕过匹配。 |
| E-07-29 | SEC-003 | empty | resolved | explicit | 07-17/07-18/07-19 | 缺principal/tenant/resource scope拒绝；tenant越权404，平台越权403且审计。 |
| E-07-30 | SEC-003 | encoding | resolved | explicit | 07-17/07-18/07-19 | 固定角色canonical ASCII和opaque tenant/resource ID严格校验，中文显示名不成为授权依据。 |
| E-07-31 | SEC-003 | concurrency | resolved | explicit | 07-17/07-18/07-19 | 角色撤销与并发请求/后台任务重核以当前PG权限线性化，旧授权不能跨越撤销后继续新操作；原已确认权利不凭撤销释放。 |
| E-07-32 | SEC-004 | empty | resolved | explicit | 07-70/07-71 | 缺加密key/版本或空密文拒绝敏感读写，不回退明文或空秘密成功。 |
| E-07-33 | SEC-004 | encoding | resolved | explicit | 07-70/07-71 | 密文/摘要按字节保真与key version关联，不trim/归一化；乱码/错误key不产生安全投影。 |
| E-07-34 | SEC-005 | empty | resolved | explicit | 07-08/07-09/07-10/07-11/07-12 | 空密码/TOTP/challenge/恢复码拒绝，未完成绑定的平台身份只能受限安全页。 |
| E-07-35 | SEC-005 | encoding | resolved | explicit | 07-08/07-09/07-10/07-11/07-12 | 密码UTF-8字节不静默trim/归一化；TOTP固定ASCII位数，恢复码canonical ASCII严格解析。 |
| E-07-36 | SEC-011 | empty | resolved | explicit | 07-67/07-68/07-69 | 空DNS答案/缺策略/目标不完整拒绝target_unreachable；不回退未经策略检查的dial。 |
| E-07-37 | SEC-011 | encoding | resolved | explicit | 07-67/07-68/07-69 | A-label ASCII主机、IPv4-mapped IPv6 Unmap、畸形/zone拒绝，所有A/AAAA结果逐一验证且仅数值拨号。 |
| E-07-38 | SEC-012 | empty | resolved | explicit | 07-70/07-71 | 中间CA密文/key/链缺失拒绝签发，根不回退到在线PG或备份。 |
| E-07-39 | SEC-012 | encoding | resolved | explicit | 07-70/07-71 | CA/签名/key材料字节严格保真，受保护文件格式版本校验，文本locale不改变签名输入。 |
| E-07-40 | SEC-013 | empty | resolved | explicit | 07-72/07-73 | 无到期且完整扫描时实际删除0是有效；缺hold/依赖查询或未执行扫描不算成功。 |
| E-07-41 | SEC-013 | encoding | resolved | explicit | 07-72/07-73 | 保留边界UTC整数时刻计算，类别canonical机读码，中文名不改变30/90/365策略。 |
| E-07-42 | SEC-006 | empty | resolved | explicit | 07-72/07-73 | 缺终态/核清/hold依赖证据一律保留，不把空关联查询当已结清或可删除。 |
| E-07-43 | SEC-006 | encoding | resolved | explicit | 07-72/07-73 | 业务ID/batch/hash逐字节匹配，编码/Unicode差异不自动合并争议证据。 |
| E-07-44 | SEC-007 | empty | resolved | explicit | 07-75/07-76 | 无base/WAL/manifest或断档备份健康失败；空目录和未运行命令不能算可恢复。 |
| E-07-45 | SEC-007 | encoding | resolved | explicit | 07-75/07-76 | WAL文件名/cluster/timeline/LSN canonical校验，ASCII hex和hash逐字节比较，畸形文件名拒绝。 |
| E-07-46 | SEC-008 | empty | resolved | explicit | 07-75/07-76 | 缺解密身份/恢复凭据拒绝恢复；空key文件不能按无加密备份继续。 |
| E-07-47 | SEC-008 | encoding | resolved | explicit | 07-75/07-76 | recipient/密文/key字节不trim或Unicode归一化，错误key/坏编码必须失败。 |
| E-07-48 | SEC-009 | empty | resolved | explicit | 07-84 | 无故障时点/业务水位/真实样本则RPO/RTO未通过，不按0或脚本退出成功。 |
| E-07-49 | SEC-009 | encoding | resolved | explicit | 07-84 | 计量整数单调时间/UTC和明确时钟误差，中文日期只展示，不用浮点截断达标。 |
| E-07-50 | SEC-010 | empty | resolved | explicit | 07-81 | 缺任一issuer/source核清证据继续F，空余额/空查询不授权新发。 |
| E-07-51 | SEC-010 | encoding | resolved | explicit | 07-81 | 逐来源金额十进制精确、source/委托/批次canonical身份；编码差异不跨源合并或抵销。 |
| E-07-52 | SUP-PAGES-001 | unclassified | unresolved | null | 07-84 | 分类器未能从补充要求摘要推导taxonomy，显式flagged assumption；完整页面/六步仍按源文计划和验证。 |
| E-07-53 | SUP-RECOVERY-001 | unclassified | unresolved | null | 07-84 | 分类器未能从补充要求摘要推导taxonomy，显式flagged assumption；完整页面/六步仍按源文计划和验证。 |

Equality: 53 surfaced = 51 authored truths + 2 explicit flagged assumptions；原始input/probe/status等字段留在07-EDGE-DISPOSITIONS.json，未伪装分类器已消除不确定性。

