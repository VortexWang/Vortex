# REQUIREMENTS-SOURCE: VORTEX V1.11 extraction

- Source: `docs/VORTEX 需求规格说明书 V1_11.md`
- Source SHA-256: `95c73f356841da9a41f9d8bd4ada008497bb1503904792d500701de1898653e5`
- Extraction date: 2026-11-03
- Coverage: 257 unique original IDs (250 numbered requirement table records, including AGT IDs in column 2; 7 outside-table blocks).
- Supplemental constraints: 20 stable IDs.
- CR-001: API load tool is Vegeta; original V1.11 source is unchanged.

## Original numbered requirement records

### GEN-001
- Source lines: 217-217
- SHA-256 of exact extracted text: `0490b9529b03b73d93c8f91db928699e81ebaef465d583bd94daa8b45e2eee89`
```text
| GEN-001 | 后端与 Edge Hub **不得**承载任何业务转发流量。 |
```

### GEN-002
- Source lines: 218-218
- SHA-256 of exact extracted text: `27f08ba1fd7fc0a65f781b82a307788ee8962de3895ffa99b4dec675f074a689`
```text
| GEN-002 | Agent 与 Engine**必须**编译为同一个可执行文件 `vortex-node`，二者只通过第 8.2 节定义的进程内接口交互。 |
```

### GEN-003
- Source lines: 219-219
- SHA-256 of exact extracted text: `10b05103aa96630e6eb44c9f394088dfcf3720a4ccafe1fa6ac72dc8d561ae4d`
```text
| GEN-003 | 转发引擎**必须**为自研独立 Go 模块，可脱离 Agent 单独测试与压测。 |
```

### GEN-004
- Source lines: 220-220
- SHA-256 of exact extracted text: `b8816af2c55cdf896a135c030d0d77726b6146f730833d8465a734729b1bd469`
```text
| GEN-004 | 转发引擎**不得**访问数据库或主动请求控制面；输入为运行配置、授权、份额租约及已验证的时间锚点，输出为应用结果、计量与事件。 |
```

### GEN-005
- Source lines: 221-221
- SHA-256 of exact extracted text: `16437e9de13fe435806afa66d5a07a111958afa07ce012a383dfe3a24e4f794f`
```text
| GEN-005 | 入口、中继、出口是同一节点端在不同规则中的角色，不是独立产品。 |
```

### GEN-006
- Source lines: 222-222
- SHA-256 of exact extracted text: `7e437a088433facbee1265441bcd9c0c61e4c1110c1210d4cc9d489433b2d164`
```text
| GEN-006 | 后端失联而 Edge Hub 在线时按 HUB-006～HUB-018 有限自治；节点与全部 Edge Hub 失联时，仅在配置与运行授权有效时运行，存在平台执行点的规则还须执行点的计费授权及份额有效；全自托管规则按 MTR-011 豁免计费与份额。任一必需条件先失效即停止，所有期限受 HUB-008 硬截止约束。 |
```

### GEN-007
- Source lines: 223-223
- SHA-256 of exact extracted text: `8eeb2a2cdbd978e8b4084c9f00f8d66879739ca4b48fb446cbd368bd21b3fdb9`
```text
| GEN-007 | 对外 API 只暴露平台自己的业务模型；运行配置与 VLink 协议属内部契约，不对外承诺兼容。 |
```

### TEN-001
- Source lines: 261-261
- SHA-256 of exact extracted text: `f549cde9ffae9ad993a533f2fbc791bd3bb5909b00a94b0b32e349a612c6a5e5`
```text
| TEN-001 | 数据模型**必须**区分 `user_id` 与 `tenant_id`；V1.0 一个普通用户对应一个租户，为后续团队能力预留。 |
```

### TEN-002
- Source lines: 262-262
- SHA-256 of exact extracted text: `88d431aa6f2c7cc42085242a58c65445028d923129751eaaaf247b40c7d60a19`
```text
| TEN-002 | 私有规则、自托管节点、个人节点组、用量与消费记录**必须**带租户归属。 |
```

### TEN-003
- Source lines: 263-263
- SHA-256 of exact extracted text: `71e6b8f437963554af43b4de190b626b83507864bcc7da59c0343e3f645a92d0`
```text
| TEN-003 | 平台节点与平台节点组归平台所有，通过权益授权给租户使用。 |
```

### TEN-004
- Source lines: 264-264
- SHA-256 of exact extracted text: `634192d18fba981d61d96288fc47054fa267b970fb985a39a46ccb94a519652e`
```text
| TEN-004 | 普通用户**不得**访问其他租户的配置、凭据与用量；隔离在数据访问层强制执行，不依赖界面。 |
```

### TEN-005
- Source lines: 265-265
- SHA-256 of exact extracted text: `d7e0731228ba89b13ce540a2804c1dd95ebacaa88b56b22d15350ce3e1dc64bd`
```text
| TEN-005 | 管理员跨租户操作**必须**进入审计。 |
```

### TEN-006
- Source lines: 276-276
- SHA-256 of exact extracted text: `36b647ac3a7797845b75576c59fa5ccb1a4ca46ae9cb6b510d9a1caf7613f5e5`
```text
| TEN-006 | 管理员对自托管节点的管理权限**不等于**可以把它分配给其他租户。 |
```

### TEN-007
- Source lines: 282-282
- SHA-256 of exact extracted text: `9ded125da86456033da92fdf55936a3614a6f0a5313145922fa708ad3c26ac6d`
```text
| TEN-007 | 账号状态只有 `active` 正常、`suspended` 暂停、`banned` 封禁。 |
```

### TEN-008
- Source lines: 283-283
- SHA-256 of exact extracted text: `b3e615db129867c2e97725fad20ab3cd4a03fd02a8ac3eae537b3e025d22ddb7`
```text
| TEN-008 | 套餐到期、流量耗尽、数量超限等作为独立的**业务限制** `restriction` 记录，不写入账号状态。 |
```

### TEN-009
- Source lines: 284-284
- SHA-256 of exact extracted text: `228fbdf4470c74aef0a56be71f6b428d681a1036faf091dd0553c0c8451806dc`
```text
| TEN-009 | 多个限制同时存在时，全部解除后才恢复业务。 |
```

### NOD-001
- Source lines: 292-292
- SHA-256 of exact extracted text: `084b9f4ca6a80c598341ac226621aca40e5cdfd0d7d55ba559c1cfce09eaaee8`
```text
| NOD-001 | 节点记录名称、区域、归属（平台 / 租户）、所属 Edge Hub、能力（可作入口 / 中继 / 出口、是否有公网地址）、对外地址、程序版本、健康状态、端口范围、容量。节点不单独配置倍率，计费倍率由所在节点组决定（NOD-014）。 |
```

### NOD-002
- Source lines: 293-293
- SHA-256 of exact extracted text: `47189f17fa2adbe9a95fbb1030d31f3e69e760147a2f5b351b2429990beab8d8`
```text
| NOD-002 | 节点生命周期：`pending` 待注册 → `online` / `offline` → `draining` 排空中 → `disabled` 已停用 → 删除；停用可恢复。 |
```

### NOD-003
- Source lines: 294-294
- SHA-256 of exact extracted text: `4c797cf2809e2a6485240226f22ea546d3dcc03ec9367d68c43cbd936016c0fb`
```text
| NOD-003 | 维护排空：停止接受新会话，默认等待已有会话最多 300 秒后关闭；管理员可调整期限，并在操作前看到受影响的规则与会话数。 |
```

### NOD-004
- Source lines: 295-295
- SHA-256 of exact extracted text: `1eb0b2f92228b81497ed6c13308dc9db252ad1ebec155d32f5a9d0c768119535`
```text
| NOD-004 | 删除前**必须**检查规则依赖、端口占用与待结算记录；删除不清除必要历史。 |
```

### NOD-005
- Source lines: 296-296
- SHA-256 of exact extracted text: `3587e9a8bc19b2a64ec52c6868f663ba4e973e45685b2fdf7fd500121df085c0`
```text
| NOD-005 | 无公网地址的节点只能作为反向建链段的下游（见 RUL-004）。入口组成员与反向建链段的上游组成员**必须**有公网地址并开放 `link` 端口；入口组加入无公网地址节点时拒绝。 |
```

### NOD-006
- Source lines: 302-302
- SHA-256 of exact extracted text: `19febb44c9e3cf73038a74d9cf7339bade1b147809dbeeeb68207e9e744b0024`
```text
| NOD-006 | 节点组用途为入口 `entry`、中继 `relay`、出口 `exit` 之一；节点可加入多个与其能力相符的组。 |
```

### NOD-007
- Source lines: 303-303
- SHA-256 of exact extracted text: `5f56320e1ff6000d48cd8cb3bf2319680e1c0ad73428c74831278065eefc1157`
```text
| NOD-007 | 平台组只含平台节点；个人组只含同一租户的自托管节点；V1.0 不混合。 |
```

### NOD-008
- Source lines: 304-304
- SHA-256 of exact extracted text: `33bd6d6fe817564891dfe846d06e73fa05dccbca4c8470d5078a8a6630b57c44`
```text
| NOD-008 | 一条规则可组合已授权的平台组与自己的个人组。 |
```

### NOD-009
- Source lines: 305-305
- SHA-256 of exact extracted text: `ee70000599e9dab5fefa9e88dc239057f765816d0d8b32afcc5ce040ec941494`
```text
| NOD-009 | **组内成员调度**：中继组与出口组配置成员选择方式，加权轮询 `weighted_round_robin` 或主备 `primary_backup`，并配置健康探测参数；上游节点按此选择下一跳成员。入口组成员由 DNS 解析分配（PRT-011），不配置调度方式，只配置健康探测参数。 |
```

### NOD-010
- Source lines: 311-311
- SHA-256 of exact extracted text: `cd62fda49d35ce957ad8a6fcb13328e01be3014bb808d3867c153422323b776d`
```text
| NOD-010 | 两个节点组之间是否允许相连，由管理员通过**段许可** `segment_permit` 显式配置；拥有两个组的使用权不自动代表允许它们相连。 |
```

### NOD-011
- Source lines: 312-312
- SHA-256 of exact extracted text: `14f25817b31957e67624026cda54f51e60211044a9759d585c60109705eb1228`
```text
| NOD-011 | 租户自己的个人组之间默认允许相连；个人组与平台组相连需平台允许。 |
```

### NOD-012
- Source lines: 313-313
- SHA-256 of exact extracted text: `24e480774ad0dd4442008ee6b8b3b6f2f91fb75da06ac12d104551c6822f616a`
```text
| NOD-012 | 段许可在后端创建规则时检查，并编译进运行配置，由转发引擎在每条会话上再次执行（ENG-020）。 |
```

### NOD-013
- Source lines: 326-326
- SHA-256 of exact extracted text: `5c7bc3042e8789de54a44e1144b25cc0029b3b754ec68100766099e957abb110`
```text
| NOD-013 | 入口组至少有一个 `serving` 成员才显示可用。 |
```

### NOD-014
- Source lines: 327-327
- SHA-256 of exact extracted text: `9d58cef08c303708eb927469c1b869f05d234745fde53461dde5b90692aedf34`
```text
| NOD-014 | **组倍率**：计费倍率只在节点组上配置，组内节点统一继承组倍率，节点本身不设倍率。节点加入多个组时，在每条规则中按其所在的那个组计算。平台组的组倍率由管理员设定，最小 0.001、步长 0.001（MTR-002）；个人组（自托管）组倍率固定为 0。新建平台组时设定的初始组倍率随组创建**立即生效**（该组尚无任何计费授权，无需提前同步）；此后的修改**必须**通过新计费版本生效（MTR-003），不得直接改写；节点加入或退出组不影响组倍率。因组内节点倍率一致，执行点在会话开始时按已批准的不可变计费快照确定路径系数；路径变化与组倍率政策变化分别产生新快照，不得通过会话建立时读取可变最新配置重算（MTR-016）。 |
```

### PRT-001
- Source lines: 335-335
- SHA-256 of exact extracted text: `85a515a14e37d46d2e12271f767896f846b5a913f564163aee21fe4abf74dc83`
```text
| PRT-001 | 节点端口分为业务端口 `service`、VLink 端口 `link`、控制端口 `control`、监控端口 `metrics` 四类，分别管理。 |
```

### PRT-002
- Source lines: 336-336
- SHA-256 of exact extracted text: `e5884b3e7ac987765043aba67c2f2e55fba7e8fad85b01af1bbcee4fc3020d18`
```text
| PRT-002 | 冲突检查按“监听地址 + 协议 + 端口号”，并处理通配地址与具体地址的交叉冲突。 |
```

### PRT-003
- Source lines: 337-337
- SHA-256 of exact extracted text: `eb583e951882b53f210cee9d8ede19ae04bc33fa98a0071aa69cdb06dcc9bd9d`
```text
| PRT-003 | 同一规则在入口组内所有成员使用同一业务端口。用户可指定端口，也可留空由系统按 PRT-004 自动分配；指定的端口不可用时创建失败并提示可用端口。 |
```

### PRT-004
- Source lines: 338-338
- SHA-256 of exact extracted text: `449324b39a83a1e9194f8183a82011c19082ed153f2f470ab9dff67eea2c0e4b`
```text
| PRT-004 | 分配流程：固定有效成员集合及其版本 → 计算各成员允许范围内的共同可用端口 → 后端事务预留 → 节点实际占用（ENG-031）→ 全部成功后提交，任一失败则撤销。 |
```

### PRT-005
- Source lines: 339-339
- SHA-256 of exact extracted text: `b2a020fb15a9b82c3869ee8b30a5544a82fa209de4893611c0cc3db1688b164a`
```text
| PRT-005 | 无共同端口时创建失败，**不得**静默排除成员。 |
```

### PRT-006
- Source lines: 340-340
- SHA-256 of exact extracted text: `1531d14577847d57d1b40c6a104e672c4cfe5d0c8c21471cfd2d38a87a19e67c`
```text
| PRT-006 | 同时启用 TCP 与 UDP 时使用相同端口号，分别预留。 |
```

### PRT-007
- Source lines: 341-341
- SHA-256 of exact extracted text: `973e60b38ba0d7baae53aa7fee5cfe0ff7eb2d896c5e96cb02c42085ab5cea48`
```text
| PRT-007 | 入口组成员变化后，必须对该组全部规则重新检查端口。 |
```

### PRT-016
- Source lines: 347-356
- SHA-256 of exact extracted text: `947c8db079bc01d5c7595aeb2b16b57662bfd3ef03b3c0ef37712901232aa571`
```text
PRT-016：规则停止时端口按下表处理。

| 触发原因 | 转发 | 端口 |
| --- | --- | --- |
| 用户手动停用规则 | 停止 | 保留，直到删除或主动释放 |
| 账号暂停、封禁 | 停止 | 保留 7 天 |
| 套餐到期、流量耗尽、降级超限 | 停止受限规则 | 保留 7 天 |
| 删除规则 | 停止 | 立即发起释放 |
| 主动释放 | 先停止 | 发起释放 |

```

### PRT-008
- Source lines: 359-359
- SHA-256 of exact extracted text: `985c17715aff8f1a48b545b892c05f8f1cc0417644e99a62194c5f110eb82dd6`
```text
| PRT-008 | 7 天保留期从限制首次生效起算；重复下发或原因变化不重置；解除后再次受限重新起算。 |
```

### PRT-009
- Source lines: 360-360
- SHA-256 of exact extracted text: `08db10da89a2dde7ec108f2aab36dd5b496d9776ecb54fe27bdafa94112bf428`
```text
| PRT-009 | 节点未确认释放前端口保持隔离，不得重新分配；DNS 摘除或控制断开不算释放证明。 |
```

### PRT-010
- Source lines: 361-361
- SHA-256 of exact extracted text: `77cee43729b09e6de54e7c70a891f9d05ba3fb3b0c47ee4b9d25c0d42007ddb3`
```text
| PRT-010 | 恢复端口已释放的规则时重新分配，并提示入口地址可能变化。 |
```

### PRT-011
- Source lines: 367-367
- SHA-256 of exact extracted text: `66773b141e167c2b408f76ecd31416dc770968188d6d916595b5a305515ff6ec`
```text
| PRT-011 | 每个入口组一个统一入口域名，解析到全部 `serving` 成员。 |
```

### PRT-012
- Source lines: 368-368
- SHA-256 of exact extracted text: `049fb7c781e667c0734a2345248aa2c6c560f6b3f9f51d1f7585a441db0b6b3b`
```text
| PRT-012 | 进入解析集合的成员**必须**已具备该组全部应运行规则的配置和全部应保留端口的占用。 |
```

### PRT-013
- Source lines: 369-369
- SHA-256 of exact extracted text: `743018a813931dc3e11797357dfcf0b03972f088369dec964e6c70c7104eb561`
```text
| PRT-013 | 新规则创建失败不得导致摘除仍在服务其他规则的成员；实际失去接入能力的成员按健康策略退出解析。 |
```

### PRT-014
- Source lines: 370-370
- SHA-256 of exact extracted text: `98dcde8c8cd48cfbdcbc7fd18cd9e97f7c6788955b13e6db7982ed67378ee71c`
```text
| PRT-014 | DNS 不保证即时切换与已有连接迁移；旧解析仍可能引流，节点必须自行执行授权与限制。 |
```

### PRT-015
- Source lines: 371-371
- SHA-256 of exact extracted text: `4e61615305107a570004c0f42fcb3bc036b37373cddf701d41a8bc347a68e8c9`
```text
| PRT-015 | DNS 服务商通过适配接口接入；V1.0 交付 **Cloudflare** 适配器并经真实增删改验证；入口记录 TTL 默认 60 秒、仅 DNS 模式（不开启代理）；API 令牌按域名最小授权并加密存储。 |
```

### RUL-001
- Source lines: 390-390
- SHA-256 of exact extracted text: `ff5b02e2849582028ffd6a80fe5786334d4df28258dcaccb07678aa68a281baf`
```text
| RUL-001 | 规则包含：名称、协议（`tcp` / `udp` / `tcp_udp`）、转发方式、入口组、业务端口（可留空自动分配）、路径、各段建链方向、一个或多个目标及权重、目标调度方式、PROXY protocol 选项（`off` / `v1` / `v2`，UDP 仅 `v2`，见 ENG-009）、规则级限制（可低于套餐）、启用状态。目标可为 IP 或域名；域名由出口（直接转发时为入口）解析，解析结果须再次通过 SEC-011 检查。 |
```

### RUL-002
- Source lines: 391-391
- SHA-256 of exact extracted text: `db069be7b8570702d1a83d613b97f6bcc07dfc07f35c6d6eecd5ac77e3e6459e`
```text
| RUL-002 | 界面统一称“规则”；规则不得绑定单个固定节点，只引用节点组。 |
```

### RUL-003
- Source lines: 392-392
- SHA-256 of exact extracted text: `803b612efc22db95d64ab32a3d7a2eaf32fe34676a4d041714dec63582c7f37f`
```text
| RUL-003 | 同一规则不得重复选择同一节点组；实际路径不得重复经过同一节点。 |
```

### RUL-004
- Source lines: 393-393
- SHA-256 of exact extracted text: `e02c94b5445790344fb3ceb5f2e65a014203699738c2b5ba5a285dba65fead34`
```text
| RUL-004 | 每段建链方向取值 `auto`（默认）/ `forward` / `reverse`。`auto` 按段判定：下游组全部成员有公网地址时取 `forward`；任一成员无公网地址时整段取 `reverse`（含混合组）。取 `reverse` 时上游组全部成员必须有公网地址，否则创建失败（`RULE_SEGMENT_UNREACHABLE`）；上下游均存在无公网成员的段不可创建。`auto` 判定为 `reverse` 不需要额外权益；下游全部有公网地址时手动设为 `reverse`（不开入站端口、隐藏出口地址）需权益包含“反向建链”。组成员变化导致判定结果变化时，按 RUL-014 作为变更重新应用。 |
```

### RUL-005
- Source lines: 394-394
- SHA-256 of exact extracted text: `22ba8096ffb9c83ecd77863ef3c8f29ccfe7192c46c2965df0410463d2c99784`
```text
| RUL-005 | 每段必须有段许可（NOD-010）。 |
```

### RUL-006
- Source lines: 395-395
- SHA-256 of exact extracted text: `4e9c6e35ee65ed50f7849b8a79b18f390dbdc848dd5922ecd1e739656e2cfee0`
```text
| RUL-006 | 任一组没有可用成员时拒绝新会话，**不得**跳过指定的中继。 |
```

### RUL-007
- Source lines: 401-401
- SHA-256 of exact extracted text: `177839dd92e456550b45d32a1b198659eaf0535f825d532c0b9b2622e69e8da4`
```text
| RUL-007 | 规则级调度只用于在多个目标之间选择：加权轮询或主备。组内成员的选择由节点组配置决定（NOD-009），规则不覆盖。 |
```

### RUL-008
- Source lines: 402-402
- SHA-256 of exact extracted text: `4f61add9245d001e047478b57964858874514f41d44045d2b554591a32262832`
```text
| RUL-008 | TCP 按连接选择路径与目标；UDP 按会话选择，默认空闲 60 秒失效。 |
```

### RUL-009
- Source lines: 403-403
- SHA-256 of exact extracted text: `d704ffce6b9a62e2dfbf766abeb333019f3e2c5410899312aa6b12170ac4cacb`
```text
| RUL-009 | 会话存续期间路径与目标保持不变；故障后新会话可重新选择；不承诺已有会话无损迁移。 |
```

### RUL-010
- Source lines: 409-409
- SHA-256 of exact extracted text: `354216b179e2ce28922f3770d1db7e8ca50d0697a80f469cb8636551ca987039`
```text
| RUL-010 | 新建或修改规则采用两阶段应用：① 校验权限、额度、端口、路径 → ② 在固定成员集合上**准备**（占用资源但不对外开放）→ ③ 全部准备成功后**提交** → ④ 各成员激活并确认 → ⑤ 全部确认后显示“运行中”。 |
```

### RUL-011
- Source lines: 410-410
- SHA-256 of exact extracted text: `0b1bfa385b425e6d30e918d272d5933645b2dd69c961c4b5ebc5ca35d5d7b3ea`
```text
| RUL-011 | 准备失败只撤销本次准备，不影响已有规则。 |
```

### RUL-012
- Source lines: 411-411
- SHA-256 of exact extracted text: `e6fbbb5e916b5d9183f17057805a6ee9df3b9b7ca0a126e75818f41e8d02bca4`
```text
| RUL-012 | 提交后部分成功显示“部分应用”并重试收敛；默认 120 秒仍未完成则触发回滚。回滚以上一已生效配置的内容生成**新的递增版本**，按 RUL-016 重新校验后走同一两阶段流程；回滚自身失败时 `sync_status` 置为 `failed` 并告警，由管理员处理，不再自动重试回滚。 |
```

### RUL-013
- Source lines: 412-412
- SHA-256 of exact extracted text: `cf7862881fa34e8eb8ecd530062faa70f4c0e3b79d1618ecb8e654faae419b81`
```text
| RUL-013 | 不承诺跨节点瞬时原子切换；失联节点显示“状态未知”，不得误报回滚成功。 |
```

### RUL-014
- Source lines: 432-432
- SHA-256 of exact extracted text: `97cf558ac3caa7f7a317521c51748d7d74fc1d2f10f81fada63dce85aefd1c3e`
```text
| RUL-014 | 变更按影响分为可热更新 `hot`、需重建监听 `rebuild`、强制停止 `stop`；会中断连接的变更必须在提交前展示影响。 |
```

### RUL-015
- Source lines: 433-433
- SHA-256 of exact extracted text: `f9e29d139147fc1f4063ab2b9b2729a27ecbab60424a462139a3b4b7b5994021`
```text
| RUL-015 | 转发配置允许回滚；运行授权、账号限制、身份吊销、计费政策、预留额度与消费记录**不得**回滚。 |
```

### RUL-016
- Source lines: 434-434
- SHA-256 of exact extracted text: `94a5c0f02483c4a75779af8655a6dcab6f2f0ccbcc8e1036dda439d7df2caade`
```text
| RUL-016 | 恢复旧配置前必须重新检查当前权限与资源；旧配置不得重新开放已失效的权限。 |
```

### ENG-001
- Source lines: 469-469
- SHA-256 of exact extracted text: `d1d879732bc32dfc4b74308c6ff5720e298f0ca7ee2c61358e1ec56f6128f6ca`
```text
| ENG-001 | 引擎对外只暴露上述接口；Agent 不得直接操作 socket。 |
```

### ENG-002
- Source lines: 470-470
- SHA-256 of exact extracted text: `2f493d7d7bf0105fcce5e855e24f4a4e39afb6388b1ad547ff4a8d8b44844836`
```text
| ENG-002 | 引擎可由测试用的模拟代理驱动，用于压测、故障注入与验收。 |
```

### ENG-003
- Source lines: 471-471
- SHA-256 of exact extracted text: `b8ac05d454675fbd7be6acec3b28520e67188c0360e31689cd456fe173f434a9`
```text
| ENG-003 | 引擎内一条统一管线：入口 → 门控 → 路由（直连或下一跳）→ 管道；四种转发方式只是配置不同。 |
```

### ENG-004
- Source lines: 477-477
- SHA-256 of exact extracted text: `26dade5befe0eb726daba547b8b6b05c882696fe17407724afd89a59c29f5a82`
```text
| ENG-004 | 直接转发的 TCP 两端均为裸 TCP 时，Linux 上**必须**走 splice 零拷贝；统计与限速不得以包装连接的方式实现。 |
```

### ENG-005
- Source lines: 478-478
- SHA-256 of exact extracted text: `5ddc61c18bf484633df48e58399fbe523ee17d6bc0e6be171ff16fb79281d181`
```text
| ENG-005 | 必须正确传递半关闭：一方向结束时只关闭对应写方向，另一方向继续传输。 |
```

### ENG-006
- Source lines: 479-479
- SHA-256 of exact extracted text: `fda85ef3e3f3b5e2d8dec7e59ddb793703672698961153df1f61778838aa81ff`
```text
| ENG-006 | 支持会话级空闲超时；TCP 默认不超时，UDP 默认 60 秒。 |
```

### ENG-007
- Source lines: 480-480
- SHA-256 of exact extracted text: `5658bebedcbcf8045377a8ef4db61651059d1962b75629a594c5cdd59d7423e9`
```text
| ENG-007 | 非零拷贝路径使用分级缓冲池，稳态转发不产生每包内存分配。 |
```

### ENG-008
- Source lines: 481-481
- SHA-256 of exact extracted text: `b8c77ff548773a8b5ac31001c754ef561aa9129f3b3032b21123d3f140f7109a`
```text
| ENG-008 | 每个 goroutine 必须归属于某个会话、链路或监听的生命周期；关闭操作幂等；会话结束后不得残留 goroutine 与文件描述符。 |
```

### ENG-009
- Source lines: 482-482
- SHA-256 of exact extracted text: `9da469e11dbb46610af98e4261d02c75522e00f05d2ad974fe3398c404fc2825`
```text
| ENG-009 | 出口（或直接转发的入口）连接目标时可按规则发送 PROXY protocol v1 / v2 头，携带原始客户端地址。 |
```

### ENG-010
- Source lines: 490-490
- SHA-256 of exact extracted text: `67e07e3f8aa67d118019ccec2aff7c639b192e80c8123bf7aca1a2f997ee33bd`
```text
| ENG-010 | 承载为 TCP + TLS 1.3 双向证书认证；节点证书由平台 CA 签发，证书携带 `node_id`；吊销列表随运行配置下发并在握手时检查。 |
```

### ENG-011
- Source lines: 491-491
- SHA-256 of exact extracted text: `ef3efcc363cdd252386858d0ea52491bd5d4f97eac95b8a1b23161dff21e3cde`
```text
| ENG-011 | TLS 完成后交换 HELLO：协议版本、`node_id`、`boot_id`、`generation`、能力位，以及对 TLS exporter 的签名，使链路绑定当前 TLS 连接与节点本次启动，防重放。 |
```

### ENG-012
- Source lines: 492-492
- SHA-256 of exact extracted text: `fde5da0d1947210de756a01ca509db9d1188bc4926dc4ab70a3bd34476e01c6a`
```text
| ENG-012 | 帧头固定 8 字节：`type(1) flags(1) length(2) stream_id(4)`，大端序；单帧载荷 ≤ 16 KiB。 |
```

### ENG-013
- Source lines: 493-493
- SHA-256 of exact extracted text: `fadc69fcab1091a4d3c6ab0b626b4e439d23c2eb66b2bf02549809c54564202e`
```text
| ENG-013 | 帧类型：`HELLO`、`OPEN`、`OPEN_OK`、`OPEN_ERR`、`DATA`、`FIN`、`RST`、`DGRAM`、`WINDOW`、`PING`、`PONG`、`GOAWAY`；保留 `PADDING` 与 flags 位供 VTP 扩展，未知类型按协议版本协商处理。 |
```

### ENG-014
- Source lines: 494-494
- SHA-256 of exact extracted text: `b489239ca2dfad866d6005c40bbd68fca8a45c1189eb5dbb1b594d93b6350c45`
```text
| ENG-014 | `OPEN` 载荷为 `rule_id`、规则版本、路径代次、`hop_index`、网络类型、已经过节点列表、原始客户端地址；不含下游地址或 `session_id`。会话标识由本链路 `stream_id` 承载，UDP 与流 ID 使用同一空间（ENG-041）；具体编码与双方 ID 分配规则须按 ENGR-004 锁定。 |
```

### ENG-015
- Source lines: 495-495
- SHA-256 of exact extracted text: `73065a794ef2ff16d7942f5515ba4d9cd41dbb46d9d23c8f4eb2a5809ebd64ed`
```text
| ENG-015 | 开流不等待确认：发送 `OPEN` 后立即发送 `DATA`；失败由 `OPEN_ERR` 返回原因码 `no_route` / `unauthorized` / `quota_exhausted` / `rate_limited` / `conn_limited` / `target_unreachable`，并逐段回传到入口。 |
```

### ENG-016
- Source lines: 496-496
- SHA-256 of exact extracted text: `ac6330309188b2db4e57159226c971c86e505757f8c87bea5e14638732d5acb1`
```text
| ENG-016 | 流控：每流初始窗口 256 KiB，可自适应增长至 4 MiB；另设链路级窗口，单流不得饿死其他流。 |
```

### ENG-017
- Source lines: 497-497
- SHA-256 of exact extracted text: `1a11bc60fdb34a19a8b08b5b07c51122d1d7acea5cff2051e5ca28448ceb1bfb`
```text
| ENG-017 | 链路模式：`mux` — 每对节点维持 2～4 条长链路，流多路复用；`pool` — 预热若干空闲链路，每链路同时只承载一个流，流结束后归还复用，池大小按空闲比例自适应；`auto` — 默认，UDP 固定用 `mux`；TCP 按该规则近 10 分钟会话时长中位数选择，短于 10 秒用 `pool`，否则用 `mux`，无历史时用 `mux`。两种模式下新会话均不增加握手往返。 |
```

### ENG-018
- Source lines: 498-498
- SHA-256 of exact extracted text: `ed16e3be208b3d3b7a8b8f0775268fa5020e7c6b3a53e9ff787f988fac3fa28b`
```text
| ENG-018 | 反向建链：下游节点按运行配置主动连接上游组成员，HELLO 后声明所服务的规则；链路建立后双向对等，上游可在其上开流。断线后下游指数退避重连。 |
```

### ENG-019
- Source lines: 499-499
- SHA-256 of exact extracted text: `a70d9a6a5c1884bc975dd7955c6d11a0ae7ef72eee6d272161c71d21a4525af7`
```text
| ENG-019 | 链路保活与失效：链路上收到任何帧即视为存活；空闲 3 秒发送 `PING`，单次等待 3 秒；自最后一次收到对端帧起 12 秒内无任何帧即判定失效，并将该对端标记为不可用。检测时间上限 12 秒，满足 NFR-016。 |
```

### ENG-020
- Source lines: 505-505
- SHA-256 of exact extracted text: `7daf40a8949bf7f289280ec66513ed3fe1c1410030abf96955c3a60fca380525`
```text
| ENG-020 | 节点收到 `OPEN` 时必须依次检查：① 规则在本节点已激活且版本、路径代次有效；② 对端 `node_id`（取自证书）属于该规则允许的上游集合；③ `hop_index` 等于本节点在该规则中的跳位；④ 已经过节点列表不含本节点；⑤ 门控条件满足。任一不满足即拒绝。 |
```

### ENG-021
- Source lines: 506-506
- SHA-256 of exact extracted text: `446ec31a00038bf8527779ce1d4d3894d42d49f5201710796d58ae9d82a5c106`
```text
| ENG-021 | 下一跳与目标只从本地运行配置选取，不得使用报文中的地址，以此保证中继不可绕过。 |
```

### ENG-022
- Source lines: 507-507
- SHA-256 of exact extracted text: `1dc4646b03793e49711e8e95bbf8b33a07632eea64d10542c941c7dff95a79a7`
```text
| ENG-022 | 选择下一跳时排除已经过的节点；下一组无可用成员时返回 `no_route`。 |
```

### ENG-023
- Source lines: 508-508
- SHA-256 of exact extracted text: `5885bc371a7e520324161554660e6779a9705288da7716742b79474e1b17393c`
```text
| ENG-023 | 每次拒绝产生审计事件，含对端 `node_id`、`rule_id` 与拒绝原因。 |
```

### ENG-024
- Source lines: 514-514
- SHA-256 of exact extracted text: `0ae1d046149c91ee96ea87ad364138092e0cc5d49706ac36db0cbd12a19a651f`
```text
| ENG-024 | 新会话须配置激活、运行授权有效。存在平台执行点时，执行点还须有效速率/连接份额与有余量的有效计费授权；授权必须匹配本会话的billing_snapshot_id及系数，运行授权须允许该快照，按MTR-016和附录B.5校验，缺少匹配授权不能借用其他路径额度。路径上其他节点校验运行授权与段许可，不要求持有执行点的份额或计费授权。全自托管规则按MTR-011执行。 |
```

### ENG-025
- Source lines: 515-515
- SHA-256 of exact extracted text: `b860ac6e1e525d355584d1d6bd80ee097fc495848aaf780221b74ff540c48db0`
```text
| ENG-025 | 租约按附录 B.1 请求绑定：本地单调请求起点 q、nonce、连接和启动标识匹配，RTT≤10秒。相对租约截止 `D_lease=q+ttl−max(0.1×ttl,10秒)`；版本有效区间、运行授权依赖、份额所有权及自治硬截止为**独立边界**，最终权限取交集。不得先按绝对边界缩短 ttl 再扣安全余量。重复ID/修订不延长 D_lease，真正续租须新请求及更高修订；墙钟回拨不能延长任何边界。 |
```

### ENG-026
- Source lines: 516-516
- SHA-256 of exact extracted text: `5059889f69d042dd667872473539e526d012a4a8dbd3fd1922be261ee5b079b2`
```text
| ENG-026 | 速率：执行点按租户维护令牌桶，速率等于当前速率份额，突发不超过 1 秒份额；令牌与预扣块共同决定单次拷贝上限，零拷贝路径保持可用。规则级限速叠加生效。令牌桶须统计“令牌耗尽时间占比”，供 LIM-009 分配使用。 |
```

### ENG-027
- Source lines: 517-517
- SHA-256 of exact extracted text: `adb16d15c5c8d87efc4c73429a3eebc6a9293e4ac85fd091d5bf3fb3cac4f02c`
```text
| ENG-027 | 连接数：执行点按租户计 TCP 连接与 UDP 会话之和；份额下调时立即拒绝超限新会话，超额部分从最近建立者开始排空，最多 60 秒后强制关闭；统计平均并发与连接槽耗尽时间占比，供 LIM-009 分配使用。 |
```

### ENG-049
- Source lines: 518-518
- SHA-256 of exact extracted text: `1757ed8fe50918415d818b385a9dc16e7f2c45dd3d4a8d88e87542e345e1cfb7`
```text
| ENG-049 | **自托管入口的限制执行**：规则入口为自托管节点时，入口侧的速率、连接数与计量不作为可信依据，执行点为路径上第一个平台节点（见术语“执行点”），由其执行 ENG-024、ENG-026、ENG-027；份额租约按 LIM-002、LIM-003 分配给执行点。规则全部节点均为自托管时，按 MTR-011 处理。 |
```

### ENG-028
- Source lines: 519-519
- SHA-256 of exact extracted text: `4a8471d5c637492a14585d46166d203f6f05cfbc3b57288269b65b56c1382425`
```text
| ENG-028 | 必需门控真正失效时拒绝新会话、关闭受影响TCP、清理UDP并发送RST。两类有明确状态转换的交接例外：MTR-014版本保护带暂停只阻止数据写入并保留会话，LIM-004同节点份额预备/CAS切换始终保持一份有效门控；不能把二者当作已失效后继续转发。账号/身份限制、运行租约或自治硬截止没有交接豁免。连接降额仅按ENG-027关闭超额部分。 |
```

### ENG-029
- Source lines: 525-525
- SHA-256 of exact extracted text: `a572c9f8577a3d095c4570bd529f5a94f3ee04339d8dddaeb47b2661796374de`
```text
| ENG-029 | 运行配置按节点生成，带单调版本，包含：监听、规则（角色、跳位、允许上游、下一组成员、建链方向、目标与权重、限制）、对端节点、吊销列表。 |
```

### ENG-030
- Source lines: 526-526
- SHA-256 of exact extracted text: `0dbc247ea240c4c9490e2d0d8064f88aec394f9abbb06fb7d3320a105e335dcf`
```text
| ENG-030 | `Prepare`：校验 → 与当前生效配置做差异 → 逐对象分类为 `hot` / `rebuild` / `stop` → 占用资源；返回分类结果供界面展示影响。 |
```

### ENG-031
- Source lines: 527-527
- SHA-256 of exact extracted text: `16a448612b50e6c2a9f313f24c53c7c6bce14cd32c57ba3bbc83c458b6e5c829`
```text
| ENG-031 | 端口占用不对外开放：TCP 仅 `bind` 不 `listen`，UDP `bind` 后不读取；冲突检查覆盖通配地址。UDP 在 `Commit` 前丢弃接收缓冲中已积压的数据报，不予转发。 |
```

### ENG-032
- Source lines: 528-528
- SHA-256 of exact extracted text: `9c1ddc0c39fe34ca8cf8507a528cb839c05faa8b92020e04a1746c79bbf705bc`
```text
| ENG-032 | `Commit`：开始监听与接收；规则表整体原子切换。分类为 `hot` 与 `rebuild` 的对象，已有会话继续使用切换前的快照及其billing_snapshot_id，不迁移至新路径授权；存在平台执行点时Commit前须备齐新快照匹配授权，新旧预留分别计入资金占用（附录B.5）；分类为 `stop` 的对象，以及权限收紧类变更（段许可撤销、目标移除、允许上游变化），已有会话按 ENG-028 关闭。 |
```

### ENG-033
- Source lines: 529-529
- SHA-256 of exact extracted text: `13256198856101edb100105211e781769966ebfc2de56a6bdd90b8c37e25a7fa`
```text
| ENG-033 | `Abort`：只释放本次预留，不影响已生效对象。 |
```

### ENG-034
- Source lines: 530-530
- SHA-256 of exact extracted text: `ae36a136f6e18e5dd5a1f5d4d45cf9aa546c4a4184e44c60c6fccfa7e9725bf5`
```text
| ENG-034 | 如实报告每个对象的实际版本与状态；部分应用、重试与回滚由后端编排。 |
```

### ENG-035
- Source lines: 536-536
- SHA-256 of exact extracted text: `51f6c04335097b69ead7652b6a5e2a733d149bc9b77d4af54c233c6e228a9044`
```text
| ENG-035 | 只计双向业务载荷：VLink 帧头、握手、保活、TLS 开销与传输重传均不计。 |
```

### ENG-036
- Source lines: 537-537
- SHA-256 of exact extracted text: `8058e49d7547396a65e099590897b194c09a843bcf9f6ec6a1ad4bce3170c056`
```text
| ENG-036 | 计量点在“写成功”处：见下表。同一角色每方向只计一次。 |
```

### ENG-037
- Source lines: 538-538
- SHA-256 of exact extracted text: `707df0c8491a861d309ad725c3b279d60c9c63bcd7cb65c8e402120523120573`
```text
| ENG-037 | 执行点在拷贝前从匹配会话计费快照的本地计费授权预扣，本次写入不超过预扣量，完成后按实际写成功字节提交，余量退回；一笔预扣只能提交或退回一次。块大小每方向从 256 KiB 起，用满翻倍至 4 MiB；版本切换处理见 MTR-014。授权只在执行点存在，由 Edge Hub **先持久化预留和签发记录再响应**，以原始字节计，按附录 B.2 换算。每个规则/执行点/计费快照独立管理额度、申请与余量水位，不跨快照合并；每个快照起步 8 MiB；此后每份为 max(8 MiB, 该规则该执行点近 1 分钟速率 × 30 秒)，剩余 30% 续申；不足时按可安全签发余额发放。中继/出口只计量不预扣。最终参数须在 M2 压测后评审确认。 |
```

### ENG-038
- Source lines: 539-539
- SHA-256 of exact extracted text: `475f2ba49dec7cc711d6e99858a12bafe2728f1872239d7132f60740c49d62af`
```text
| ENG-038 | UDP 按整包预扣，发送成功后提交，失败整包退回。 |
```

### ENG-039
- Source lines: 540-540
- SHA-256 of exact extracted text: `f0815152f4b2fcef5d5f06718e79deb9493167c85a8f9c41cc9f19bd39d0e5bb`
```text
| ENG-039 | 引擎在内存累计原始字节：执行点按“规则 × 角色 × 方向 × 计费快照 × 计费授权”累计，其他节点按“规则 × 角色 × 方向”累计；每个消费批次携带billing_snapshot_id，节点/Hub入账均验证与grant_id绑定一致；不匹配则隔离为disputed并保留预留，不按最新配置重算。Agent 每秒持久化。内存取走与 SQLite 提交采用可重试批次及确认机制，提交成功前保留带稳定批次 ID 的不可变待确认副本，经 AckUsage 后释放，重复批次不重复计数。断电仍可能丢失最后一个未落盘窗口，必须按 MTR-015 以授权剩余上界形成不确定用量记录，不得声明故障时能精确恢复所有实际字节。 |
```

### ENG-050
- Source lines: 541-541
- SHA-256 of exact extracted text: `0f0043287931c5c6748cec9058fbc30ed9668119599824107d5cd0e7401e2be8`
```text
| ENG-050 | 空闲授权按规则/执行点/计费快照管理：无会话10分钟缩减到1MiB常驻额度，无会话24小时回收；缩减及回收须最终账本证明未用，不能释放未知负债。无可用计费授权时同步申请最多200毫秒，失败拒绝；该等待及MTR-014中已预发授权的版本保护带是NFR-007的例外，保护带不等于无授权继续消费。 |
```

### ENG-040
- Source lines: 556-556
- SHA-256 of exact extracted text: `7f46cbc247139ff49d1e15e82fd361fceb34cfc37a71e86e419ed4c325763126`
```text
| ENG-040 | 入口会话键为“监听 + 客户端地址”；会话表分片存储，空闲超时回收。 |
```

### ENG-041
- Source lines: 557-557
- SHA-256 of exact extracted text: `69612adc402c6526729ee2253c8cda54c0e2cc95d6f7dccfa5d640ba90d1e101`
```text
| ENG-041 | 隧道内 UDP 用 `DGRAM` 帧承载，帧头 `stream_id` 字段即该会话的 `session_id`；多个 UDP 会话复用同一链路，不为每个会话建立独立连接。 |
```

### ENG-047
- Source lines: 558-558
- SHA-256 of exact extracted text: `713ac22cc82cb26cf2f79d2b6978a4c5d480ae8151b9f5c1c479223a5ce1d6b1`
```text
| ENG-047 | **UDP 会话开流**：每个 UDP 会话在首个 `DGRAM` 前**必须**发送网络类型为 `udp` 的 `OPEN`，接收方按 ENG-020 完成全部检查后才接受该会话的 `DGRAM`；与 TCP 相同，`OPEN` 后可立即发送 `DGRAM`，被拒绝时返回 `OPEN_ERR`。未开流或已关闭会话的 `DGRAM` 一律丢弃并计入指标 `vortex_engine_dgram_dropped_total`。 |
```

### ENG-048
- Source lines: 559-559
- SHA-256 of exact extracted text: `59f2d0022c437e442d154ec864a3c0396fa6bedd589ce7e0bc5a6b1dcd59d265`
```text
| ENG-048 | **数据报大小与调度**：单个 UDP 数据报载荷超过单帧上限（ENG-012）时丢弃并计数，不分片。`DGRAM` 不受流级与链路级窗口约束，使用独立的发送队列（默认每链路 1 MiB），发送时优先于 `DATA` 帧，但每轮写入中 `DGRAM` 不超过 50% 的字节，避免饿死 TCP 流；只有该队列满时才丢弃 `DGRAM`。混合负载下 UDP 须满足 NFR-005。 |
```

### ENG-042
- Source lines: 560-560
- SHA-256 of exact extracted text: `ccf05cceac3cdb2b98282dd666de51248ccdfef87efb9d83b4a1e071b4edda72`
```text
| ENG-042 | 出口每个会话使用独立的已连接 UDP socket 访问目标，保证回包正确归属。 |
```

### ENG-043
- Source lines: 561-561
- SHA-256 of exact extracted text: `15f4b36ee3c9c3bc924f84e5e451474c726f80cce191979cb1e10356ca6d0073`
```text
| ENG-043 | socket 收发缓冲可配置，默认 4 MiB；应支持批量收发以降低系统调用开销。 |
```

### ENG-044
- Source lines: 567-567
- SHA-256 of exact extracted text: `4ccd8db0ffdc9913d89852c2dde7eb81d59efe4d1a18f1af575557fb10fbb2f8`
```text
| ENG-044 | 帧载荷与 TLS 记录大小对齐，写入合并以减少系统调用；使用硬件加速的 AES-GCM。 |
```

### ENG-045
- Source lines: 568-568
- SHA-256 of exact extracted text: `9cc653cf4b5bed1f1ad0426a3f508b149dfdd2ecebba300390feb19051ff34ed`
```text
| ENG-045 | goroutine 预算：每 TCP 会话 ≤ 2 个；每条链路读写各 1 个；每个 UDP 监听 1 个；出口每 UDP 会话 1 个。 |
```

### ENG-046
- Source lines: 569-569
- SHA-256 of exact extracted text: `1ba44a6d6a8c9614231adbc8a647267fa6b88c5ce15da4c279a3fd32cfbf50df`
```text
| ENG-046 | 性能与稳定性指标见第 13 节，引擎必须在启用正式计量与门控的情况下达标。 |
```

### AGT-001
- Source lines: 579-579
- SHA-256 of exact extracted text: `7b4b56de294eabb696064c80fd77d407318ae0eb9895ecdf1e324d59603b6d08`
```text
| 注册 `enroll` | AGT-001 | 使用一次性注册令牌（默认有效 30 分钟）换取节点证书；私钥在本地生成，只上传证书签名请求。 |
```

### AGT-002
- Source lines: 580-580
- SHA-256 of exact extracted text: `9c6fbfcc2d41e988288606637b7a313f6ea5350c9f17e3f053943d5f99363f1b`
```text
| 控制 `control` | AGT-002 | 以 mTLS 与 Edge Hub 保持 gRPC 双向流；断线指数退避重连，可切换到同区域其他 Edge Hub。 |
```

### AGT-003
- Source lines: 581-581
- SHA-256 of exact extracted text: `8ebe1560c734fc83f32f2ec3790d7328bc1b804145c3f41731cb9c614607d3be`
```text
| 存储 `store` | AGT-003 | 用 SQLite（WAL）保存：已生效运行配置、授权与租约、计量账本、待上报记录、事件。 |
```

### AGT-004
- Source lines: 582-582
- SHA-256 of exact extracted text: `965228811a6db13385b5f598cc1426ae71dfd5e0fa58d1beaa3683636dc210f9`
```text
| 协调 `reconcile` | AGT-004 | 比较期望版本与实际版本，驱动引擎 `Prepare` / `Commit` / `Abort`，回报逐对象结果。 |
```

### AGT-005
- Source lines: 583-583
- SHA-256 of exact extracted text: `124ffde940f46f2087d4c8c4413ed410138e9414b9379e03002e5866407518ea`
```text
| 授权 `grants` | AGT-005 | 运行授权每 60 秒续期，计费授权按水位申请，份额按期续租；全部按 ENG-025 请求绑定 TTL。与 Edge Hub 建立附录 B.1 的认证时间锚点；本地墙钟偏差 > 5 秒只告警，不能修改截止点。时间锚点误差区间过大或重启无法恢复可信时间时，不得签收跨绝对边界的新授权，须重新校时握手；不允许以一条未计时的 Hub 时间消息替换本地墙钟。 |
```

### AGT-006
- Source lines: 584-584
- SHA-256 of exact extracted text: `560b38cbd611388ed3118c9351df37dd14efb331c5f20fa283ddbf6b6c003d1a`
```text
| 上报 `report` | AGT-006 | 每秒从引擎取得可重试计量批次，SQLite 提交成功后回执；每 10 秒上报，使用 `node_id`、`boot_id` 与递增序号幂等确认。已上报账本保留不少于 7 天；未结清或有争议的记录不清理。重启时按 MTR-015 上报各授权最后持久化累计值及剩余上界；空间不足或提交失败须停止业务并保留未确认记录，不得只停止新会话而让旧会话继续生成无法记录的消费。 |
```

### AGT-007
- Source lines: 585-585
- SHA-256 of exact extracted text: `f15f3b75169c19dd6cd232b58bd8825d3e530cf8275dd3b9b443421d6ecff512`
```text
| 探测 `probe` | AGT-007 | 对配置中的下一组成员与目标做周期探测（默认间隔 3 秒，连续 3 次失败标记不可用，连续 2 次成功恢复），结果供引擎选路，并上报用于前端按段展示延迟。 |
```

### AGT-008
- Source lines: 586-586
- SHA-256 of exact extracted text: `957c1e0b1d3aad8c486802dc0d30de0b11e9f990ef51c9147f80581838c63274`
```text
| 升级 `upgrade` | AGT-008 | 检查兼容性与磁盘空间 → 排空 → 替换二进制 → 失败自动恢复旧版本与兼容配置；不可逆迁移必须阻止自动回退。 |
```

### AGT-012
- Source lines: 587-587
- SHA-256 of exact extracted text: `0631cc615cfd73c301eeec757be8afbfaaa13063530468284979a7bf227faa3f`
```text
| 证书 `cert` | AGT-012 | 节点证书默认有效期 30 天，剩余 1/3 时自动续签（私钥在本地重新生成，经 Edge Hub 提交 CSR）；续签失败时告警并按退避重试；证书过期后不得建立新的控制连接与 VLink 链路。 |
```

### AGT-009
- Source lines: 593-593
- SHA-256 of exact extracted text: `0b27076b42bcf52af33bc6c7124499171223ba9b6f0536cfbe97e91f705bf4e8`
```text
| AGT-009 | 每次启动生成新的 `boot_id`；旧授权仅用于补报、结算，不得继续消费。先上报旧启动账本和各授权剩余上界；旧启动续租权终止及份额安全转移按 LIM-010，不能只凭新 boot_id 推断旧进程已停止。 |
```

### AGT-010
- Source lines: 594-594
- SHA-256 of exact extracted text: `aea074229b71ce1725d20eeccdca7cc1dd6d4baba9d83d2d5259b49dbf7a8105`
```text
| AGT-010 | 启动恢复按“恢复并Prepare配置→取得启动份额/运行与计费授权→Commit→激活回执”推进；份额候选由LIM-009的持久化期望状态和准备结果确定，不要求本次实际Commit。有平台执行点的路径须该执行点完成门控准备；无平台路径按MTR-011豁免。配置准备失败、启动取消与超时按附录B.3释放本次引用，不影响其他规则已持有的份额。 |
```

### AGT-011
- Source lines: 595-595
- SHA-256 of exact extracted text: `2db17c4c28410cda078824e57da034e045a6a84f962c60b207a7667e7ad6f57f`
```text
| AGT-011 | 控制断开后只在必需授权、份额和 HUB-008 硬边界内转发；不能仅因运行授权尚未到期忽略计费耗尽、份额失效或自治截止。 |
```

### HUB-001
- Source lines: 601-601
- SHA-256 of exact extracted text: `3536b7ec072f76ff0103432d123adf27e7fccc111dd0ba36b9482fc73906c3d8`
```text
| HUB-001 | 终结 Agent 的 mTLS gRPC 长连接，校验节点身份与吊销状态。 |
```

### HUB-002
- Source lines: 602-602
- SHA-256 of exact extracted text: `522ce1caf63466ac5024b5d973cea32dedd360d21cdebd756cbe0269d1331f92`
```text
| HUB-002 | Edge Hub 始终为运行授权和计费授权的签发方，使用后端同步的规则/限制与本实例区域额度。运行配置仅由后端生成。份额在线由后端生成，自治时仅可在附录 B.3 的逐节点所有权范围内原值续租，**禁止跨节点再平衡**；不得增加份额总量、放宽版本或自治截止点。 |
```

### HUB-003
- Source lines: 603-603
- SHA-256 of exact extracted text: `0552ff03b19da5f5dfdd581982d8d7edc2554662e19e289ad6c5bc24c1d946bd`
```text
| HUB-003 | 计量批次先写本地 SQLite（WAL）。在线时转发后端并获确认后再向 Agent 回确认；自治模式下本地落盘即向 Agent 回确认，批次留在自治账本（HUB-012）待恢复后上报。两种情况下都保证不丢不重。 |
```

### HUB-004
- Source lines: 604-604
- SHA-256 of exact extracted text: `cd7526426aee68af964a40bdcf4d2cf3486743365678620b9bb94c8274075d12`
```text
| HUB-004 | 缓存本区域节点在线状态、实际版本、健康度与探测结果，供后端快速对账。 |
```

### HUB-005
- Source lines: 605-605
- SHA-256 of exact extracted text: `8325edb876e07b2b59b7f32ac3dff95a83aee04458e85864dc7d7cecd2e8144c`
```text
| HUB-005 | 主动以 mTLS gRPC 连接后端；Edge Hub 宕机不影响转发，Agent 切换到同区域其他 Edge Hub 后重发未确认数据。 |
```

### HUB-006
- Source lines: 611-611
- SHA-256 of exact extracted text: `e50c8cc16a6a6fec7412dff1f2dbfa700a87493960d8206a7f8deb4113b072d5`
```text
| HUB-006 | 按“租户×Hub实例×资金来源”委托计费字节，委托ID和来源ID不可复用。实例目标为近1小时平均计费速率×自治上限；零用量激活/准备规则按 min(64MiB,可分配额÷符合条件实例数)设启动目标，先预留启动目标再按用量切分。每个来源新委托增量≤MTR-004的A_s，合计≤ΣA_s；不发尚未生效的未来月额度。低于30%补充、每10分钟重算；在线回收需持久化停止对应签发权和账本高水位证明，失联按HUB-015。恢复账本轮转只转接同一委托中的未签发余量，不视为补充。 |
```

### HUB-007
- Source lines: 612-612
- SHA-256 of exact extracted text: `d40577f834c9d6ecb2419d190d5ca758db288398526d1158e25bee5958b056da`
```text
| HUB-007 | 连续 30 秒无法通信时声明自治，但自治计时从**最后一次成功的后端心跳**起算，后端与 Hub 均持久化该确认心跳标识。自治仅续期既有规则、在已委托额度/逐节点份额所有权内签发、处理安全重启、持久化计量和接收重连；不得修改配置、端口、DNS、注册新节点或跨节点转移份额。 |
```

### HUB-008
- Source lines: 613-613
- SHA-256 of exact extracted text: `7747a9a51819b04554d06d43c30817aab7b41ed191fd1b7c742c8ee34c9b04fc`
```text
| HUB-008 | 自治硬截止为最后成功后端心跳+自治上限（默认12小时）。所有运行、计费和份额实际权限均独立裁剪到该绝对边界；相对ttl不因靠近此边界而缩短后再扣余量（ENG-025）。硬截止持久化，重启和恢复账本轮转不能重置或延长；可信时间未恢复禁止签发。已知限制或必需资源更早失效则更早停止，边界后没有额外5分钟尾期。 |
```

### HUB-009
- Source lines: 614-614
- SHA-256 of exact extracted text: `238571afbe237b040aec49279f483de2cba951347fefff48f8c34e7b9be238da`
```text
| HUB-009 | 自治执行已知的账号限制、套餐/流量包到期和余额耗尽；资金来源到期后不得再从该来源未签发部分发授权。已经发出的承诺按 MTR-013 处理，但对应运行权限仍须在已知套餐到期时停止。月度重置只使已知旧来源到期，不预发下月新额度；恢复后由后端补算。 |
```

### HUB-010
- Source lines: 615-615
- SHA-256 of exact extracted text: `4bc54c17b24c059ad18bac9a0b20bfdeacfad4e05552e7391ada84adf7a03084`
```text
| HUB-010 | 失联期间新封禁/吊销不晚于最后成功后端心跳+自治上限生效。恢复通信先下发吊销/限制，再按HUB-013进行账本轮转和对账；轮转期间没有权限豁免，不复活已截止授权。 |
```

### HUB-011
- Source lines: 616-616
- SHA-256 of exact extracted text: `ae785ed62e8b73051230297ff13c9a68de803c60794c534db380e39370ba77ae`
```text
| HUB-011 | 实例计费额度隔离；控制连接切换不释放余额，不增加份额。旧计费授权在原边界内有效，新授权仅从新签发实例合法持有的额度签发。相同节点/boot_id的同一份额链可以按LIM-004经允许的Hub续租；新Hub必须纳入该链的允许签发集合，自治不跨节点再平衡。资源不足或所有权不明则暂停并说明原因。 |
```

### HUB-012
- Source lines: 617-617
- SHA-256 of exact extracted text: `80c2e9d55a79b99e957ff18c7fb204925d560f590d6500cd498ac580363ab9bb`
```text
| HUB-012 | **自治账本**：Edge Hub 的全部签发、续期、结算与计量批次写入本地 SQLite（WAL）账本，带 Edge Hub 实例 ID 与递增序号；账本保留不少于 7 天，且对账完成前不得清理。 |
```

### HUB-013
- Source lines: 618-618
- SHA-256 of exact extracted text: `2abc37567dba9ea1ddf43b462f6d6e10621879f9078099a6ebd10fb76de77f43`
```text
| HUB-013 | 恢复通信后先执行吊销/限制，进入 recovering 模式；按附录B.4原子封存旧签发账本前缀至S，将**同一委托**的未签发余量R以唯一转接记录交给后继 signer_epoch，旧在外授权A继续由原预留覆盖。后继epoch可立即在R、原份额链、原配置及原自治硬截止内续签，不等完整对账、不新增委托、不延长旧所有权；新尾账本持续落盘。后端幂等重放前缀与尾部至确认检查点K，F不得因转接重复增加或释放；核清租户后才允许新委托/新所有权并恢复normal。不能证明原委托/转接唯一性或时限时停止签发并冻结。 |
```

### HUB-014
- Source lines: 619-619
- SHA-256 of exact extracted text: `0eac66a55e8445435dcc4b136fc98ffb343a50d6c38bf30f140cc23947c2b99a`
```text
| HUB-014 | Agent在当前Hub自治且有同区在线Hub时60秒内切换；report包含原签发方、signer_epoch、委托/授权ID、已确认消费、未知上界、份额链代次及截止。只接受当前连接新响应；切换本身不释放资源。recovering模式按自治的冻结资源与配置约束执行，不对外误报已完成对账。 |
```

### HUB-015
- Source lines: 620-620
- SHA-256 of exact extracted text: `80425cc3f34767e8e50b9dca6ddb47586a9a35b218fefb63d83c8cbba4d2a704`
```text
| HUB-015 | 失联实例的区域委托和来源负债冻结；仅在对应签发权已不可逆关闭/安全截止，且完整账本或同等未消费证明齐备后释放已证明未用部分。账本前缀封存不等于整份委托停止：若有HUB-013后继epoch，仍按转接记录占用，不能释放R或把S当最终末序号。单纯超时、Agent当前授权清单都不是释放证明，永久丢失按MTR-009/MTR-015处置。 |
```

### HUB-016
- Source lines: 621-621
- SHA-256 of exact extracted text: `a2623a32949f4c4ef36d3c3d9a2c06335e969f774c0066e8cd340f0bb6f21455`
```text
| HUB-016 | 仅以逐来源A_s补充接管实例计费额度；旧来源由L覆盖的冻结负债不能跨来源抵占新套餐。份额按LIM-004在同一链内续接或等跨节点安全释放；不能用新计费额度绕过份额占用。证据不足时显示“额度待结算”/“份额待接管”，不承诺无证明固定时长解除。 |
```

### HUB-017
- Source lines: 622-622
- SHA-256 of exact extracted text: `404c0875018e308d7880cda414ca9e767ef50a8bc12b54c8f60c20b3f6a4b78d`
```text
| HUB-017 | 旧实例恢复先执行HUB-013，核验签发权和账本链；已关闭或已转接给后继epoch的余量不得再次用于旧epoch，重启须幂等恢复唯一转接事务。已封账授权不能复活，迟到精确账本只做幂等冲正和平台风险返还，不重复扣费或补偿。 |
```

### HUB-018
- Source lines: 623-623
- SHA-256 of exact extracted text: `1619180eccc3e8446516c21badd9f2d6ac02e259e81656360627617deb75ca21`
```text
| HUB-018 | 计费政策至少提前24小时同步相关Hub；节点授权在T前最后一个有效租约周期预发，并保证相对运行/计费租约覆盖T及版本保护带。所有旧版实际消费受valid_until=T独立限制，不把ttl先截为T−q。未确认预同步的签发方须先安全关闭或其原硬截止≤T，否则推迟T且重新满足24小时提前量。保护带与缺失授权按MTR-014分别处理。 |
```

### BE-001
- Source lines: 646-646
- SHA-256 of exact extracted text: `39a53f9796b66655d9edbce74cddab2c98c16d9dc6d5175fe7281d75a5c94461`
```text
| BE-001 | PostgreSQL 是业务与期望配置的唯一事实来源；Valkey 数据丢失不得重置余额或丢失已确认业务。 |
```

### BE-002
- Source lines: 647-647
- SHA-256 of exact extracted text: `08ac15d1b2ccb945757b0709d759d6157a73d99f3de54ed6d2bb9ce05a7f779f`
```text
| BE-002 | **规则编译器**是业务规则模型到运行配置的唯一转换点，输出每个节点的角色、跳位、允许上游、下一组成员与建链方向；编译结果可重放、可测试。 |
```

### BE-003
- Source lines: 648-648
- SHA-256 of exact extracted text: `885783ff77381f2c27e895521aa9490712b85be37f6b56e4976284bbbc9d58fa`
```text
| BE-003 | 规则应用、DNS 变更、通知等异步动作必须经任务表 + Outbox 发出，保证与业务事务一致。 |
```

### BE-004
- Source lines: 649-649
- SHA-256 of exact extracted text: `d20f38f0b9c8e244bc3cac2df48cb7be49b62908d4ff765ab9591c024cd3fffe`
```text
| BE-004 | 所有租户数据查询在仓储层强制按 `tenant_id` 过滤。 |
```

### BE-005
- Source lines: 650-650
- SHA-256 of exact extracted text: `e2cfb46243ec4c665c36b72d8f16e17835f6c6980b51863515faa3d7f88526a0`
```text
| BE-005 | 对外 API 为 REST，以 OpenAPI 为契约并生成前端类型；状态推送使用 SSE。 |
```

### BE-006
- Source lines: 651-651
- SHA-256 of exact extracted text: `d39ec10abd14f432c615786f16dc879ae9a3686195dda3cbcdabcbbb0257e1a1`
```text
| BE-006 | 对账性能要求见 NFR-019。 |
```

### PLN-001
- Source lines: 665-665
- SHA-256 of exact extracted text: `bfabd57cbf48995b8b961f3017acd256d22734e88bba71a6610a57aed141fba4`
```text
| PLN-001 | 有效能力取各来源授权的交集，数值限制取最小值。 |
```

### PLN-002
- Source lines: 666-666
- SHA-256 of exact extracted text: `e26b88fd9d4f740f56f1e05e4694797807eee72cf2efeed314c30a13e7558357`
```text
| PLN-002 | V1.0 每个用户只有一个主套餐。 |
```

### PLN-006
- Source lines: 667-667
- SHA-256 of exact extracted text: `e4070b4ce129033d575d13cc705e7b7288a5044e0112d866785925a6774211b5`
```text
| PLN-006 | 基础流量周期 `traffic_reset` 为 `none` 或 `monthly`；默认时长 ≤31 天取 none，其余 monthly。monthly 自首次开通的 UTC 锚点逐月计算，目标月份无对应日则取该月末日（如 1 月 31 日→2 月末→3 月 31 日），不以截短后的日期滚动。每期单独资金来源，期末未承诺余量作废、不结转；在外承诺按 MTR-013。换不同套餐建立新锚点，同套餐续费和时长码保持原锚点。 |
```

### PLN-007
- Source lines: 668-668
- SHA-256 of exact extracted text: `0973c9110fb9fb16bf0c959d98d11a7d07c326c8eb5d22c548155541be5a2de6`
```text
| PLN-007 | 消费顺序：先扣主套餐当期基础流量，耗尽后再按 PLN-003 的顺序扣流量包。 |
```

### PLN-008
- Source lines: 672-682
- SHA-256 of exact extracted text: `e8513dec77c42a414abbd1c41514d7e2088056d859f4ae7272c89440a4d7d0c6`
```text
PLN-008：权益码兑换效果按下表执行。

| 兑换类型 | 效果 |
| --- | --- |
| 同套餐套餐码 | 有效套餐从原到期日累加时长：none 增加码面流量并保留当前余额至新到期日；monthly 仅延长有效期，当前期余额和重置锚点不变，不提前发未来月份流量。已过期套餐从兑换时重新开通并建立新周期，旧过期余额不复活 |
| 不同套餐套餐码 | 确认后替换；从兑换时起计算新周期与基础流量；旧时长不折算，旧基础流量作废，流量包保留；提交前明确展示损失 |
| 时长码 | 只延长尚有效的当前套餐；无主套餐或已到期时不可用，不复活旧余额、不变更流量重置锚点 |
| 流量码 | 增加一个独立流量包 |

| 编号 | 要求 |
| --- | --- |
```

### PLN-003
- Source lines: 683-683
- SHA-256 of exact extracted text: `e70478c643a35178188a5ee904308af2790397e2559a89d14e1d70bad4371776`
```text
| PLN-003 | 流量包独立记录余额与到期时间，按最早到期优先消费，到期相同按发放时间；主套餐无效时暂停消费，流量包期限照常计算。 |
```

### PLN-004
- Source lines: 684-684
- SHA-256 of exact extracted text: `4d199c845ca15f1c23bd21f9c0aab1d0f9ef049e02f9fa9c2a7802a631a1a8ad`
```text
| PLN-004 | 权益码具有有效期、使用次数、适用条件、作废与审计；重复请求不得重复发放（幂等键）。 |
```

### PLN-005
- Source lines: 685-685
- SHA-256 of exact extracted text: `4bc60352c3743b1a7fbcc50704912f34e99d52bbad07b4785cd908b10e0c6ff3`
```text
| PLN-005 | 降级前展示将失去的能力、超限规则、新限制与作废额度；不合规规则暂停不删除，端口按 6.2 保留；数量超限时优先保留用户选择，未选择时按创建时间从早到晚保留。 |
```

### MTR-001
- Source lines: 693-693
- SHA-256 of exact extracted text: `00fb3d0b916276c0c32ef1d6b352d7301c34a8d72cc1b60086953b79c4496a74`
```text
| MTR-001 | 各角色流量按 ENG-035～ENG-038 采集；只有执行点的流量用于计费，中继与出口的流量只用于展示、对账与异常检测。 |
```

### MTR-002
- Source lines: 694-694
- SHA-256 of exact extracted text: `51d5544228e22ae67162a9b665876c5f4c79c5a7e9ec6610769f26eae4e9381b`
```text
| MTR-002 | 原始和计费流量分别保存，路径系数精度为千分位正整数（平台组最小 0.001，自托管为 0）。按授权累计计算，不逐包向上取整；签发预留与结算口径按附录 B.2。跨授权的小数余量按租户和资金来源累计，用户最终扣减取累计整数部分，未满一字节余量保留；向上取整仅用于安全占用，不额外向用户收费。 |
```

### MTR-003
- Source lines: 695-695
- SHA-256 of exact extracted text: `76172163b8c478e9fe10046981c03cb8b9a899fdad65ea946c834b03ea458c66`
```text
| MTR-003 | 版本政策采用半开区间[valid_from,valid_until)，倍率变更至少提前24小时；新建组初始版本可立即生效。政策边界按HUB-018确认，两版预发授权各占实际资金，不可重复预留。同一会话在T前后按MTR-014切换，不因相对租约安全余量必然提前10秒停止旧计费版本。 |
```

### MTR-004
- Source lines: 713-713
- SHA-256 of exact extracted text: `ae05263efaed716a04e0a90edf4c1a2c5467703b066a1e604f06c33d9d009e58`
```text
| MTR-004 | 每个不可复用资金来源s分别记录有效余额B_s、未结算委托F_s（含在外授权/未知负债但不重复相加）、旧承诺保障L_s，必须F_s≤B_s+L_s。来源可用时A_s=max(0,B_s−F_s)，未生效/过期/暂停来源A_s=0；可新委托额为ΣA_s，**不是**max(0,ΣB_s−ΣF_s)。每次新增委托同时满足各来源增量≤A_s；L_s不可新发或跨来源调拨。B=ΣB_s、F=ΣF_s、L=ΣL_s只作汇总。结算/冲正同事务更新原来源，旧消费不扣新套餐。 |
```

### MTR-005
- Source lines: 714-714
- SHA-256 of exact extracted text: `d9acf77d0dd0e974c41b6f6f9fcbeeedc8c531302dd81c5093dce22788a1b10c`
```text
| MTR-005 | 余额被未结算预留占用时，界面显示“额度待结算”。 |
```

### MTR-006
- Source lines: 715-715
- SHA-256 of exact extracted text: `bf4c1b06a912cab7357856cba3bb8c5e5dc4897b339b55efd152d80a69390e23`
```text
| MTR-006 | 上报按 (`node_id`, `boot_id`, 序号) 幂等入账，重复上报不重复扣减。 |
```

### MTR-007
- Source lines: 716-716
- SHA-256 of exact extracted text: `07b8c229d3980e65c46258140955a9edd8aeb223ef4a225812dabfe685a9cae0`
```text
| MTR-007 | 重启后旧授权 settling，新启动只从签发方尚未签发的有效额度申请，不能复用旧授权余量；先按 LIM-010 终止旧启动续租权并安全交接。旧消费不确定时按 MTR-015 冻结，与新启动账本分开。 |
```

### MTR-008
- Source lines: 717-717
- SHA-256 of exact extracted text: `ecfcee1f7ce2dcc05ba640f1297b150aed2d2b84ebc408c816e241d40b4de8d4`
```text
| MTR-008 | 计费授权耗尽或任一节点授权失效时按 ENG-028 处理，拒绝或关闭整条路径上的会话；执行点已写成功的数据照常计费，不回滚。 |
```

### MTR-009
- Source lines: 718-718
- SHA-256 of exact extracted text: `a460bfe9ea65082abd97d408dcf138834f1ed5c9d016eb1783db6497afd1b309`
```text
| MTR-009 | 争议满 72 小时后启动保守封账，不能仅凭最后确认值释放全部余量。有最终账本和未消费证明则精确结算、释放余量；无法证明的部分按 MTR-015 记录不确定上界。平台承担补偿有已批准且足额的风险预算时，才能在同一事务内封账并补偿；否则保持 disputed 并告警，待人工审计处置，72 小时不是强制释放期限。任何人工调整须给出资金来源、原因及前后守恒校验。 |
```

### MTR-010
- Source lines: 719-719
- SHA-256 of exact extracted text: `b087370bc313145f19b0095da74989e6b0c6898c3c9aee612cc70658ac0be437`
```text
| MTR-010 | 创建与编辑规则时，界面**必须**显示该路径的路径系数及构成（各组组倍率），套餐说明中注明“流量 × 路径系数”的计费方式。 |
```

### MTR-011
- Source lines: 720-720
- SHA-256 of exact extracted text: `97479e795c9ad95dc6105c13d030272f13218447da6b05f68cd001a48808904c`
```text
| MTR-011 | 全自托管路径没有平台执行点，免计费授权、套餐速率/连接份额及流量额度门控；其转发资源限额只校验规则数，租户可自选本地限速但不作为平台可信份额。仍须配置合法、账号可用、主套餐有效、运行授权有效、身份未吊销、段许可有效，并遵守自治截止；不是免账号或套餐有效性校验。有平台节点的路径仍在第一个平台节点计费和限额。 |
```

### MTR-013
- Source lines: 721-721
- SHA-256 of exact extracted text: `5b8c98404d38a077298db8d1d2d3a410e9062f41d26259655b17dc99e406697c`
```text
| MTR-013 | 被动减少按来源失效未承诺余额；在线实例在对应签发权已停且账本高水位确认后回收已证明未签发部分；失联委托冻结。旧来源不可即时撤销的承诺建立该来源L_s保障，消费仍归旧来源，不追缴、不扣替换套餐或新月来源。新套餐、每月新周期、流量包和风险补偿使用独立不可复用来源ID，分别按A_s可委托；旧L_s不冻结新来源。权限仍按账号/套餐有效性停止。 |
```

### MTR-014
- Source lines: 722-722
- SHA-256 of exact extracted text: `2c731b319c5e8a5bc928ea85324a79821e2801620de7919489a5721796ec317a`
```text
| MTR-014 | 版本有效区间独立于相对租约：UTC上界达到T停止旧版新增写入/预扣；UTC下界达到T才可开始新版。已预发新版且运行/份额有效时，仅因时间误差产生的保护带允许保留TCP/UDP会话、暂停业务写入，最多12秒，期间不消耗新旧授权、不释放连接槽。准确时钟和零RTT时在T原子切换，无固定10秒空窗。旧在途限长写最多1秒取消，已提交内核的写按原预扣最终回执只计一次；未用量退回。新版缺失时仍只同步申请200毫秒，不能借12秒延长申请；超过保护带、旧写无法终止或必需运行/份额/自治边界失效即按ENG-028关闭。双方相对租约须覆盖切换窗口（HUB-018），保护带不能延长任何硬权限。 |
```

### MTR-015
- Source lines: 723-723
- SHA-256 of exact extracted text: `b278648584b554edb6272819ab7fb19a01c1a5e77d133b7a4a5ec2ef7f05b643`
```text
| MTR-015 | **故障不确定用量**：对每份授权保存可确认原始累计量 C_raw、原始总额度 G_raw；未知原始上界 U_raw = G_raw − C_raw（排除已证明退回部分），对应未知计费上界 U_bill 为该授权尚未结算的安全预留计费字节。U_bill 继续计入 F 并单列，不计入用户实测消费；Hub/节点均不能以“授权过期”释放 U_bill。精确账本到达后按实际值结算、释放已证明未用量；72 小时后无法确认则仅在足额平台风险预算获批并扣减时，以 U_bill 作保守费用封账且在同一事务按附录 B.2 补偿用户被扣的有效来源额度，用户净扣费只含已确认消费。补偿计为新的、有平台资金来源的发放，不能伪装余量退回；无预算则保持冻结。迟到精确账本只能幂等减少平台损失，不重复增加用户余额。上界封账不表示精确实测，NFR-021、ENGR-006 以此为故障计量边界。 |
```

### MTR-016
- Source lines: 724-724
- SHA-256 of exact extracted text: `9ead1b0054475582f155d7b893848b4fd49abfbcad934d43e244e9725583b051`
```text
| MTR-016 | 计费快照由租户/规则内单调且不可复用的rule_path_revision、路径组有序序列/角色、各组政策版本及倍率和精确系数k组成，billing_snapshot_id唯一标识不可变内容并绑定租户/规则/执行点。每笔预留、消费、上报、final及不确定负债必须引用同一快照和k；不得让不同快照共享原始额度或以最新路径重算。路径或计费政策变化产生新快照；旧会话按原路径快照在当前合法政策内续签，政策边界按MTR-014切到同旧路径的新政策快照。新会话只用已Commit配置的快照；回滚生成更高路径修订及新快照，不复活旧授权或已撤销权限，不清除既有负债。细则见附录B.5。 |
```

### LIM-001
- Source lines: 730-730
- SHA-256 of exact extracted text: `06a939a73cd7a25dff9c23396ebac118b33d02fdb7d7ed83e81a233174f9d7e6`
```text
| LIM-001 | 运行租约相对ttl上限5分钟，每60秒续签，按ENG-025一次性折减。计费、份额、版本及自治权限以独立边界取交集，不以给ttl反复减余量表达依赖。过短/已过期的相对租约不得接受；版本保护带及份额CAS均不能绕过运行租约失效。 |
```

### LIM-002
- Source lines: 731-731
- SHA-256 of exact extracted text: `6232439d32fe0f88c6103ee357911ddcab7627706777609835117ec19b1fa85d`
```text
| LIM-002 | 用户速率为全部平台执行点双向合计；后端以 PostgreSQL 事务分配按 `(tenant_id, node_id)` 唯一归属的速率份额及所有权代次，不能按 Hub 切换重复计入或发放。 |
```

### LIM-003
- Source lines: 732-732
- SHA-256 of exact extracted text: `50c29892919fa8c64ddf65fc7e72371cf3450ed5bb8cf0c648b687234bd53760`
```text
| LIM-003 | 用户连接数为全部平台执行点 TCP 连接与 UDP 会话总和，按与速率相同的逐节点所有权模型分配；全自托管路径按 MTR-011 豁免。 |
```

### LIM-004
- Source lines: 733-733
- SHA-256 of exact extracted text: `7c4738823405f1f79f5f61e4c84ee786c4c818774459a44bb7dbd0c2994e0be0`
```text
| LIM-004 | 份额按租户/节点/kind建立只绑定同一boot_id的线性代次链。自治只原值续租，不跨节点再平衡。相同节点/boot_id正常续期、调额或延长ownership_until采用附录B.3的“预留max(a,b)→准备后继但不可消费→关闭全部旧续租签发权（旧子租约继续有效）→节点持久化最低代次并原子CAS→回执后释放差额”；门控不中断且任何时刻只有一个活跃代次。增加量先从可用总份额预留；降额差额须执行确认，连接降额还须排空完成才给其他节点。跨节点/boot_id或缺少完整关闭证明时，禁止CAS捷径，等停止证明或旧所有权安全截止。 |
```

### LIM-005
- Source lines: 734-734
- SHA-256 of exact extracted text: `1e2afd9ccf05bb5280b3ee31f6fa11742eb6450b209e69e5718330b842c6c27b`
```text
| LIM-005 | 用户全部执行点的突发容量之和不超过 1 秒用户速率额度；规则可设置更低限制。 |
```

### LIM-006
- Source lines: 735-735
- SHA-256 of exact extracted text: `a6af02a783543966d297394d66d3d78b5714dd242252acb88ec8463a550af851`
```text
| LIM-006 | 下调同节点份额按LIM-004预备后原子换用新代次，不能先使数据面旧租约失效。速率降额在切换回执后才释放差额；连接降额只拒绝超额新会话并按ENG-027排空，降至新额度后才确认并释放差额。旧签发方不可达则保持其所有权占用，直到安全关闭或截止；显示生效中，不误报完成。 |
```

### LIM-007
- Source lines: 736-736
- SHA-256 of exact extracted text: `9731170dd4a211310851c870eebc9d1e1b4aa189803c7c57ea3182a619ba0584`
```text
| LIM-007 | 授权携带ID/修订、nonce、node_id、boot_id、generation、signer_epoch及份额代次。相对租约回收按签发时刻+ttl+安全余量作为无确认情况下的保守界；有持久化关闭旧续租权及节点CAS执行回执时，同链已替代代次按B.3释放，不必停有效新门控。未知消费不因租约回收释放，仍须HUB-015账本证明。重复/迟到低代次不得覆盖新状态。 |
```

### LIM-008
- Source lines: 737-737
- SHA-256 of exact extracted text: `a89e78285cef3268fd8c6c5c25efa301ab5820251b8fb4c127743a9077718101`
```text
| LIM-008 | 限制执行记录submitted/enforcing/enforced/failed；配置准备、份额prepared不等于已生效。CAS丢回执时查询持久化最低代次与引擎实际代次，不重复叠加份额。首次bootstrap在Commit许可可能送达后、未获可靠结果时持久化activation-unknown并继续占用；许可到期不是未激活证明，按B.3查询与安全终结，不能仅凭actual_version未更新回收。真正必需运行授权失效仍自行停止，不能将交接状态当作离线无限宽限。 |
```

### LIM-009
- Source lines: 738-738
- SHA-256 of exact extracted text: `f8044f56a253498d581b81fad27954bb7d43c611927e39338989b7f9b77fbc9a`
```text
| LIM-009 | 候选以持久化desired_enabled=true、当前权限合法且已成功Prepare的目标配置/成员快照确定，不依据actual_version、Commit结果或历史会话。首次候选先按B.3分配bootstrap预留，计入全局占用，方能Commit；已发许可的未知激活引用持续计入占用，禁止以准备超时再发给其他节点。每60秒按候选n分配：n=0不发，保底50%均分，其余按速率用量×(1+令牌耗尽占比)或平均并发×(1+连接槽耗尽占比)分配；零需求均分。整数向下取整、余数稳定分配；连接槽不足则真实排队。既有同租户同执行点份额按引用复用，不按规则重复发；可调整部分才经CAS重分，自治原值续租。 |
```

### LIM-010
- Source lines: 739-739
- SHA-256 of exact extracted text: `90f3f1d188c6fb4daa2728a4a2aa0a2f68fa78be472d48f7b08f412b902d076c`
```text
| LIM-010 | 新boot_id不是同链无中断CAS对象。所有允许签发方先持久化禁止旧启动续签，可信取得旧进程停止及连接清空证明，或等其最后可能有效租约/所有权安全截止后才绑定新启动；不可达签发方仍可能续签时等ownership_until。旧计费授权仅结算；新签发只用有效未签发额度，代次变化不清零负债。 |
```

### FE-001
- Source lines: 759-759
- SHA-256 of exact extracted text: `f94c48c02139ba34f8028236570027c0dd8b56e375a22317ad22a2c173ac8301`
```text
| FE-001 | 全中文界面，简洁，面向业务而非运维细节。 |
```

### FE-002
- Source lines: 760-760
- SHA-256 of exact extracted text: `5e53cf881b474366a0cb6b33d70eed7c9d0251a498afc3492f4bb0f198640ace`
```text
| FE-002 | 创建入口放在列表页按钮，不作为导航栏目；列表页不常驻创建表单。 |
```

### FE-003
- Source lines: 761-761
- SHA-256 of exact extracted text: `1a1cb8d7505e72be96cc39378682265b753b51e4b5f0608ab69d9ac577d8c634`
```text
| FE-003 | 规则列表第一列为名称、第二列为入口地址。 |
```

### FE-004
- Source lines: 762-762
- SHA-256 of exact extracted text: `4a57b88eccf578140e49385bdc1698ff7c6284cbaed9531589878a517e326d87`
```text
| FE-004 | 规则状态分三栏展示，由 SSE 实时刷新：“已保存”对应 `desired_version` 已持久化；“已应用”对应 `sync_status`（含“部分应用”“状态未知”）与各节点 `actual_version`；“可用”对应 `run_status` 与 `restrictions[]`（含“授权待确认”“额度待结算”“计费切换中”“份额待接管”）。五层数据须可展开查看，不得合并为单一状态。 |
```

### FE-005
- Source lines: 763-763
- SHA-256 of exact extracted text: `e872e53099658e83cd4080bfe6343d00ece4c53e2e2826387cff4613411126d3`
```text
| FE-005 | 路径编辑器：入口组 → 0～3 个中继组 → 出口组，只提供有权限且有段许可的组合；每段建链方向可选“自动（默认）/ 正向 / 反向”，权益不含反向建链时只显示“自动”。 |
```

### FE-006
- Source lines: 764-764
- SHA-256 of exact extracted text: `7b9032c9b50fb1629fb305525a6f706348224c9547c93a694fa06441331589ae`
```text
| FE-006 | 连通性按段展示实测延迟，未测得显示“—”，不填估算值。 |
```

### FE-007
- Source lines: 765-765
- SHA-256 of exact extracted text: `d7c9d61ed5422f4a128b71ffbca2b1070186a50cdde10b9368e9eb795aff8d25`
```text
| FE-007 | 变更提交前展示影响分类（热更新 / 重建监听 / 强制停止）与受影响会话数。 |
```

### FE-008
- Source lines: 766-766
- SHA-256 of exact extracted text: `da2ee5ea2190fdc70b8dd653d94ff3f5d69a508292c312529ff757ecaade8dea`
```text
| FE-008 | 错误提示包含原因与处理建议，错误码映射为中文文案；链路失败按段指出原因。 |
```

### FE-009
- Source lines: 767-767
- SHA-256 of exact extracted text: `50403f5115a8d07e1d234674e2c785f1ae4fa090879d7b71bd47bdb48bf6c673`
```text
| FE-009 | 套餐替换、降级、释放端口等不可逆操作必须二次确认并展示损失。 |
```

### FE-010
- Source lines: 768-768
- SHA-256 of exact extracted text: `d501330960f6deb65cdff9c261f52c463c1dcbdebe2e7bfbd50d5dcb114333a4`
```text
| FE-010 | 站内通知覆盖套餐到期、流量不足、节点离线、配置失败。 |
```

### FE-011
- Source lines: 769-769
- SHA-256 of exact extracted text: `c9dea63e1976e27f60b34524fd8f7478a58678fb5da5a797b77168dbf0f47694`
```text
| FE-011 | 流量展示时效见 NFR-020。 |
```

### FE-012
- Source lines: 770-770
- SHA-256 of exact extracted text: `71cbb118c663561d8d2722d7170868cb3513979bab554ee8cf8db5a63c3b2868`
```text
| FE-012 | 任一区域处于自治或recovering模式时，用户端与管理员端显示“控制面降级”横幅，说明影响范围（相关区域、不可变更的操作）；此时规则创建、修改、删除按钮置灰并说明原因。 |
```

### SEC-001
- Source lines: 778-778
- SHA-256 of exact extracted text: `6cdb86164d85c952d3ac28a90890eb4867b7b9ff9421e30bc7cbb9e912ad2bdf`
```text
| SEC-001 | 组件间（后端 ↔ Edge Hub ↔ Agent）与节点间（VLink）一律使用双向证书认证；注册凭据与运行身份分离。 |
```

### SEC-002
- Source lines: 779-779
- SHA-256 of exact extracted text: `f959fd4f4d0957c5cb911d7bd1ce03d542644e35115abd27aeadcd32ce068ae5`
```text
| SEC-002 | 身份吊销后必须阻止重连、续期与新链路建立；吊销列表随运行配置下发。 |
```

### SEC-003
- Source lines: 780-780
- SHA-256 of exact extracted text: `f873cf1fab0e68f540541f76a58c28ef60d4d3d722efe168a83b09844751bbd9`
```text
| SEC-003 | 权限三层强制：① 用户 API 以会话 tenant_id 限定资源，他人资源返回 404 RESOURCE_NOT_FOUND；管理端越权返回 403 ACCESS_DENIED 并审计；② 租户表读写经 tenantscope（BE-004），CI 禁止绕过封装的直接查询；③ 节点只执行发给自己 node_id 的运行配置，按 ENG-020～ENG-024 校验开流、端口、路径和许可。不再依赖未交付外部测试编号，验收以 13.5 为准。 |
```

### SEC-004
- Source lines: 781-781
- SHA-256 of exact extracted text: `a1163d7789233a678a81e6192409edfbc44a51c0e2211a1f4b9eee956b28aaaa`
```text
| SEC-004 | 密钥、令牌、DNS 凭据不得进入普通日志；敏感字段加密存储。 |
```

### SEC-005
- Source lines: 782-782
- SHA-256 of exact extracted text: `965f894cd7d213105527d84e7d30b34d914e77ccb02cf01ee42eff90fe3ab31d`
```text
| SEC-005 | 用户登录支持密码 + 二步验证（TOTP）；登录与敏感操作限流。 |
```

### SEC-011
- Source lines: 783-783
- SHA-256 of exact extracted text: `c3dcbb6a79dc0df9b472f4d8a42b9a9894f207836fdf75953f3d1bdd41dba20c`
```text
| SEC-011 | **目标地址限制**：规则目标（含域名解析结果，每次建连时检查）**不得**为：回环地址、链路本地地址（含 `169.254.0.0/16`、`fe80::/10` 等云元数据地址）、组播与保留地址、本节点自身地址、平台组件（后端、Edge Hub、数据库、Valkey）地址，以及节点自身的 `link` / `control` / `metrics` 端口。私有网段默认禁止，由管理员按节点组放开（如出口位于内网）。管理员可配置额外的地址与端口黑名单（如 25 端口）。以上“本节点自身地址与回环地址”的限制只适用于平台节点；自托管节点作为出口时允许访问本机服务，但其 `link` / `control` / `metrics` 端口仍不可作为目标。命中时规则创建失败，或该会话以 `target_unreachable` 拒绝并记录审计。 |
```

### SEC-012
- Source lines: 784-784
- SHA-256 of exact extracted text: `6dd165f0e60e13adeeddd4bd5d3a5ee0e648ddf3aded0d7ba052b9e456cc9010`
```text
| SEC-012 | **证书体系**：平台 CA 分两级：根 CA 私钥离线保存，只用于签发中间 CA；中间 CA 签发节点与组件证书，私钥由 KMS 或加密文件保存，不以明文进入数据库。节点证书有效期与续签见 AGT-012。 |
```

### SEC-013
- Source lines: 788-799
- SHA-256 of exact extracted text: `7773c0d6446c4acc1f5e4a59e97904c6922e3c1d3d2da19123fed33e078b4896`
```text
SEC-013：数据按下表保留。

| 数据 | 默认保留 |
| --- | --- |
| 普通日志 | 30 天 |
| 控制任务与应用明细 | 90 天 |
| 分钟级流量 | 90 天 |
| 日级流量、结算、兑换记录 | 365 天 |
| 管理审计 | 365 天 |

| 编号 | 要求 |
| --- | --- |
```

### SEC-006
- Source lines: 800-800
- SHA-256 of exact extracted text: `00649bd3349548fa43fb83dc0f9fda5dccf2d93463f5d73f1d98630c0f4dee5d`
```text
| SEC-006 | 未结算、未完成与争议记录不按普通期限自动清理。 |
```

### SEC-007
- Source lines: 806-806
- SHA-256 of exact extracted text: `e905037d5dc119a5db9beabd762c3af422da5d86b00677177a6252a2c95c3a38`
```text
| SEC-007 | 每日全量备份 PostgreSQL，并持续归档 WAL 以支持时间点恢复；覆盖平台配置、受保护的外部凭据与迁移信息。中间 CA 私钥加密后单独备份；根 CA 私钥离线保存，不进入数据库及其备份（SEC-012）。 |
```

### SEC-008
- Source lines: 807-807
- SHA-256 of exact extracted text: `0da9dc2571a09b2e4bbbf0945c443af32243ec011d9bd1857b3536ed705de349`
```text
| SEC-008 | 密钥备份加密；恢复凭据与备份文件分开保管。 |
```

### SEC-009
- Source lines: 808-808
- SHA-256 of exact extracted text: `f2d83bf28a50cee5eb23bd0cea7d574f82ca2344bffa27d6ccf37c6bfbbef6a1`
```text
| SEC-009 | 默认 RPO ≤ 15 分钟（WAL 归档），RTO ≤ 4 小时。RTO 与恢复对账时间（NFR-023）之和必须小于 Edge Hub 自治上限（HUB-008，默认 12 小时），以保证恢复期间业务不中断。 |
```

### SEC-010
- Source lines: 823-823
- SHA-256 of exact extracted text: `fc7b98d964c341b103bc3e114630432399b7520012a7e25f3d084bf672e337f1`
```text
| SEC-010 | 不得直接按备份中的旧余额重新发放额度。 |
```

### NFR-001
- Source lines: 848-848
- SHA-256 of exact extracted text: `c06130707a156afbd9070f509e2b16d23b1d840c65f22a90e6ba3bdadaaed2be`
```text
| NFR-001 | 直接转发 | ≥ 700 Mbps | ≥ 直连的 70% |
```

### NFR-002
- Source lines: 849-849
- SHA-256 of exact extracted text: `adcc3b9aef576e9addad9c7cb0b526ba37729ed0fa49ca1c18a7cdaf1a4dbb83`
```text
| NFR-002 | 正向隧道 | ≥ 600 Mbps | ≥ 同协议同加密最小基准的 70% |
```

### NFR-003
- Source lines: 850-850
- SHA-256 of exact extracted text: `d066314b8fa0c518dff82199fa752f7a4572da0a89434dbd770b5456893c8d1a`
```text
| NFR-003 | 三中继多跳 | ≥ 400 Mbps | ≥ 同路径最小基准的 70% |
```

### NFR-004
- Source lines: 851-851
- SHA-256 of exact extracted text: `544964c987bbf1450f0e8c3d08a205e249953d9f42cac877eaa1a88fa5d0f712`
```text
| NFR-004 | 反向隧道 | 与同路径正向隧道差异 ≤ 5% | — |
```

### NFR-005
- Source lines: 854-854
- SHA-256 of exact extracted text: `83909f71195dde9e191785c87d4f025a0c295ab02f4ad5d50b5f1a1b423c7ac8`
```text
- NFR-005：UDP 1,200 字节数据报、100 Mbps，丢包 ≤ 0.1%。
```

### NFR-006
- Source lines: 855-855
- SHA-256 of exact extracted text: `3e6331c0d248bdda57b414defddd94a68faaed0b0a95dc6619ca58d4534e4b58`
```text
- NFR-006：大文件校验一致；多跳顺序正确且不可绕过；伪造的开流请求全部被拒。
```

### NFR-007
- Source lines: 856-856
- SHA-256 of exact extracted text: `81d86fd64198d08cca07673f7335a4aa91ae4fbeb3801294d3e4a67546afc46f`
```text
- NFR-007：隧道新会话不因开流确认或握手增加往返。测量方法：从客户端发起连接到收到目标首字节的时间，减去路径上各段 RTT 与出口到目标 RTT 之和，差值 ≤ 5 ms（中位数，pool 与 mux 模式分别测量；不含 ENG-050 所述无可用计费授权及 MTR-014 版本边界保守等待的情形）。
```

### NFR-008
- Source lines: 864-864
- SHA-256 of exact extracted text: `eb41cca163be4fe0e71513648cb1a1848fc2fb9876894240db8f80bf8494b46e`
```text
| NFR-008 | 无崩溃、死锁、持续积压与重复计费。 |
```

### NFR-009
- Source lines: 865-865
- SHA-256 of exact extracted text: `18144b828d80850a300eb59ee1adc18d78131d13c7c42f8d7821113afbb0a78e`
```text
| NFR-009 | 后 6 小时平均常驻内存不超过前 6 小时平均值的 120%。 |
```

### NFR-010
- Source lines: 866-866
- SHA-256 of exact extracted text: `014a36743a8cc3afa0019a2a7d30c6f4ac95f8719502ddaeadf0bbfdc82399b9`
```text
| NFR-010 | 清空测试会话 10 分钟后，连接与会话归零。 |
```

### NFR-011
- Source lines: 867-867
- SHA-256 of exact extracted text: `eb651e97c4a248a27e28cd0bd32831f84bf01d4b3c4d63b909eb5726d9ddd80a`
```text
| NFR-011 | 文件描述符与 goroutine 分别满足：测试后数量 ≤ 空闲基线 + max(⌈空闲基线 × 10%⌉, 20)。 |
```

### NFR-012
- Source lines: 875-875
- SHA-256 of exact extracted text: `1ec440202ecea14cd35ea44ce15f732b2a8e964d7f51c1b8bed624d92ad01439`
```text
| NFR-012 | 单规则创建、更新 | 95% 在 10 秒内应用 |
```

### NFR-013
- Source lines: 876-876
- SHA-256 of exact extracted text: `20d24a4ab08222fc632440f301c9f2279c05e52355afa812dbc448accf8deb11`
```text
| NFR-013 | 在线停止 | 95% 在 10 秒内生效 |
```

### NFR-014
- Source lines: 877-877
- SHA-256 of exact extracted text: `2498f94045e6ed1631a3513167eef924eae7f559b8b50afdfce2282faede6dd5`
```text
| NFR-014 | 在线节点开始执行限制 | 95% 在 10 秒内开始；连接降额排空最多 60 秒 |
```

### NFR-015
- Source lines: 878-878
- SHA-256 of exact extracted text: `3abd1d775b57aa362f6e64fcd9054e51b7b3132b3dc55fcdc450fd8b3aeedddd`
```text
| NFR-015 | 持续配置失败 | 120 秒后触发回滚 |
```

### NFR-016
- Source lines: 879-879
- SHA-256 of exact extracted text: `e47bd6d48b24a395ecd0e30e248a7d3a40374df351012b66d06858f1d73ad8f0`
```text
| NFR-016 | 节点 / 链路故障检测 | ≤ 15 秒（设计值 12 秒，见 ENG-019、AGT-007） |
```

### NFR-017
- Source lines: 880-880
- SHA-256 of exact extracted text: `c7368aa863f6160df49999bbc1bfc66ee85c76aea53a1cfffec6d61d878b5cd9`
```text
| NFR-017 | 检测后新会话恢复 | ≤ 15 秒（需存在完整替代路径，不依赖 DNS 缓存） |
```

### NFR-018
- Source lines: 881-881
- SHA-256 of exact extracted text: `2a212db62f348994847a30297e740bd151666442d71322a46139ab00996e4f54`
```text
| NFR-018 | 节点与全部 Hub 失联 | 节点与全部 Hub 失联后不晚于最后运行授权 5 分钟上限停止，且不得跨原自治硬截止；延迟响应不能延长期限。 |
```

### NFR-019
- Source lines: 882-882
- SHA-256 of exact extracted text: `d6af0c74cb4bb95b73a619dbbd7a0045d40efdeee5ca238cc47ba769808bded8`
```text
| NFR-019 | 1,000 条规则正常对账 | ≤ 60 秒 |
```

### NFR-020
- Source lines: 883-883
- SHA-256 of exact extracted text: `986e80108e8cc740428b9c255c38314daf315ec16745bcbbc1ed3071eda5a0ec`
```text
| NFR-020 | 流量展示延迟 | 95% 在 30 秒内 |
```

### NFR-021
- Source lines: 884-884
- SHA-256 of exact extracted text: `969905cbd3da9d85e17eaf4428dc4a76d0b7724de5630df4565bb4755d98008f`
```text
| NFR-021 | 计量与故障负债 | 正常可确认消费的载荷计量误差 ≤0.1%，重复兑换、上报和结算不重复发放/扣减。故障不确定上界与实测流量分列，不纳入“实测误差”掩盖；必须按 MTR-015 全额保留负债或由平台预算补偿，用户不得为无法确认的上界多付费。 |
```

### NFR-022
- Source lines: 885-885
- SHA-256 of exact extracted text: `3dd1980ab2a113918a58af53cbd6c6cef69b090ac5f6aacc88f555b8ee005924`
```text
| NFR-022 | 后端失联自治 | 在余额、逐节点份额及其有效期限均足以覆盖测试窗口，套餐未到期且无其他限制的前提下，后端失联后至最后心跳+12小时的保守自治截止前持续转发；各必需权限更早截止时以更早者为准；不因进入自治产生空窗，到截止时已停止。重启节点在安全交接证明及额度具备后 60 秒内恢复；无法证明旧启动停止时按 LIM-010 等待，不承诺故障发生后 60 秒强行接管。配置变更被拒并提示降级。 |
```

### NFR-023
- Source lines: 886-886
- SHA-256 of exact extracted text: `73aa9b4abdd28aeb93a957e5e9055d2b94d3cde31ac03e9cfeb58e475c225e52`
```text
| NFR-023 | 后端恢复对账 | 账本可得且在规模基线内时，恢复30分钟内追平至明确检查点K，前缀与尾账本幂等、不漏持久化记录；追平期间按HUB-013原委托续签，不等待全量静默。不可得部分30分钟内识别为disputed并冻结，不能误报全部完成；吊销/封禁10秒内下达。 |
```

### NFR-024
- Source lines: 887-887
- SHA-256 of exact extracted text: `5253e2624cac3c45047e5f445751d510aae36549adedc3ab75defa720695b671`
```text
| NFR-024 | 数据库恢复 | 先基础备份+WAL物理恢复，再维护对账；原委托未签发余量、份额链及自治硬截止足以覆盖窗口时，按HUB-013连续服务且不延长5分钟租约。未知资源保持隔离；WAL缺口内兑换事实不明不重新兑换。 |
```

### NFR-026
- Source lines: 888-888
- SHA-256 of exact extracted text: `8b91e69e6247a2a35ce43228472fc71f46f50c1822ecd519548caff5a8b94dae`
```text
| NFR-026 | Hub 分区、宕机与回收 | 单实例分区时 Agent 60 秒内切换控制连接；计费额度和逐节点原份额可用时业务持续。部分 Agent 留在旧 Hub 同时新 Hub 在线时也不得超发/超额续租。安全回收的完整证明具备后 1 分钟内完成已证明未用额度回收；单纯到期不得释放未知负债，不承诺无证明时固定期限恢复。 |
```

### NFR-025
- Source lines: 892-908
- SHA-256 of exact extracted text: `7c1040b1767847120239a11b16f2982b5dacfb7ccbbf5e2fe2330139f085aca6`
```text
NFR-025：以下场景**必须**逐项验证通过。

- 租户隔离与节点归属：用户端接口以他人资源 ID 调用一律 404；管理员把自托管节点或含个人组的节点转归属到其他租户被拒绝并留审计（TEN-004、TEN-006）。
- 权限三层校验（SEC-003）：① 以他人资源 ID 调用 API 得 404、管理端越权得 403 且写入审计；② CI 静态检查能拦下绕过 `tenantscope` 的直接查询（含一个故意违规的样例分支）；③ 节点拒绝不在其运行配置内的开流请求并记原因码。
- 目标地址限制（SEC-011）与自托管入口的限制执行（ENG-049）。
- 伪造 UDP 会话的 `DGRAM` 被丢弃（ENG-047）；TCP 满载时同链路 UDP 仍满足 NFR-005（ENG-048）。
- 在存在可安全调整余量且其他执行点有可回收份额时，被限速执行点在 3 个续租周期内获得更多份额；冻结份额不得强行重分；全在线调额使用同主体CAS，不全量断会话（LIM-009）。
- 后端失联自治、区域额度耗尽、自治上限到期与恢复对账（HUB-006～HUB-013）。
- 单实例分区、单实例宕机、自治中 Edge Hub 重启、自治中节点重启、自治期间计费版本切换（HUB-014～HUB-018、LIM-010）。
- 端口冲突、保留与安全释放；用户指定端口的不可用提示与可用端口建议（PRT-003）；入口组成员变化（加入 / 摘除 / 转不可用）后该组全部规则完成端口复检，复检失败成员退出解析集合而不摘除仍在服务其他规则的成员（PRT-007、PRT-012、PRT-013）。
- 规则准备失败、部分应用与回滚；回滚后运行授权、账号限制、身份吊销、计费政策、预留额度与结算记录保持不变（RUL-015）。
- 到期、耗尽、降级与离线授权；套餐到期、月度重置、更换套餐后，未承诺来源作废，已确认可回收部分收回；其余建立逐来源 L 保障并冻结，不扣入新套餐、不重复新发（MTR-004、MTR-013）。
- 节点重启后的冻结余额、旧启动停止证明、同boot份额CAS、不确定用量与平台风险补偿（MTR-015、LIM-010）。
- 反向建链断线重连与故障切换。
- DNS 解析集合与有效成员一致。
- 空环境部署、升级失败恢复、备份恢复；记录 RPO 与 RTO 实测值，并校验 `RTO + 恢复对账时间(NFR-023) < Edge Hub 自治上限(HUB-008)`（SEC-009）。

```

### NFR-027
- Source lines: 915-915
- SHA-256 of exact extracted text: `b6ab46de3f737de454878a018ffc8042d41c6cdf54d3e94907b07f739bb7adf4`
```text
| NFR-027 | T前预发新版且运行/份额租约覆盖切换。零RTT/准确时钟跨T无固定提前失效；允许时间误差下只暂停写入且在12秒保护带内恢复同一会话，旧在途只结算一次。新版缺失仅等200毫秒，硬权限失效不宽限；详见NFR-039。 | R01 |
```

### NFR-028
- Source lines: 916-916
- SHA-256 of exact extracted text: `5ca345df0da352803b1122d6f2b5b98167f1cf53b4169c32a035940221c8b943`
```text
| NFR-028 | 委托 100 MiB、消费 60 MiB 未入账后失联；即使超时也不把 100 MiB 重发。覆盖旧 Hub 已本地结算、账本尚未同步和签发快照在途情况 | R02 |
```

### NFR-029
- Source lines: 917-917
- SHA-256 of exact extracted text: `b6ea1830073bfcd58666268c663581703f541355002b98ab78cdec7866c8aaf7`
```text
| NFR-029 | A/B各50，只有A切Hub，旧Hub不能把A份额给B；正常同节点50→60→40及延长ownership_until按CAS不断流，迟到旧响应不复活。连接降额排空完成前差额不重分；详见NFR-040。 | R03 |
```

### NFR-030
- Source lines: 918-918
- SHA-256 of exact extracted text: `2991dc73a4f0f20bf1a538d362313cb9ef87f1f73bfd10ca7c65bdaf9d4f8b73`
```text
| NFR-030 | 按来源验证F_s≤B_s+L_s，旧来源B=0/F=80/L=80，新套餐B=50/F=0时可新委托50；实际新发不能挪用L，旧消费不扣新来源。部分余额减少时旧源A_s=0但不跨来源冻结；详见NFR-043。 | R04 |
```

### NFR-031
- Source lines: 919-919
- SHA-256 of exact extracted text: `121acbe91e0e3bec231635376c3b1ccbd56fdfd81b8aef8834ecdf4ebbd8557f`
```text
| NFR-031 | 在引擎取批、SQLite 提交、上报确认各边界断电；已持久化量幂等，未确认量有授权上界覆盖；有风险预算才补偿封账，无预算保持冻结；迟到精确量不重复扣费/补偿 | R05 |
```

### NFR-032
- Source lines: 920-920
- SHA-256 of exact extracted text: `9db178cc6f62f7b6f9997164a2947d9d63281d12fdfad2c4a3b29aa96e4e654c`
```text
| NFR-032 | 真实PG先物理恢复再逻辑补账，覆盖WAL缺口和独立generation；对账耗时10～30分钟时由HUB-013后继signer_epoch用同一委托R连续续签，签发账本前缀/尾部及转接重启幂等，详见NFR-041。 | R06 |
```

### NFR-033
- Source lines: 921-921
- SHA-256 of exact extracted text: `d59fa8919fe404b70a5ad3381781ff0a5e795c40eae4126b78c6037ec264cd92`
```text
| NFR-033 | TTL=300 秒，延迟响应 90 秒应拒绝；10 秒内响应的截止从请求起点算；重复 ID、不匹配 nonce、旧连接/旧启动、墙钟回拨均不能延长权限 | R07 |
```

### NFR-034
- Source lines: 922-922
- SHA-256 of exact extracted text: `4886a61883d393ae0b5006a7ccd86956c313ae77386b9c7b625989864986259d`
```text
| NFR-034 | 空租户/空节点首条规则：desired_enabled已存、Prepare成功但actual未Commit仍能取得bootstrap并Commit；准备失败/取消/超时不漏份额，n=0与连接槽不足不除零/误报可用，详见NFR-042。 | R08 |
```

### NFR-035
- Source lines: 923-923
- SHA-256 of exact extracted text: `4d9dc048428d9fcc42fe6e8d21baf507f97d63b824e907fcc3c2cad9d08633ea`
```text
| NFR-035 | monthly 重置前一天同套餐续费不增加当期流量；次日按原锚点重置；none 续费累加；过期重开、1月31日及闰年保持周期定义 | R09 |
```

### NFR-036
- Source lines: 924-924
- SHA-256 of exact extracted text: `78d3c67e188a994ce7dd59e959d5ce5ea7b7d5e03dde6635ffdca3062f82e6bd`
```text
| NFR-036 | 在硬截止前最后续租周期发放授权、Hub 自治中重启、节点时钟回拨；全部节点不晚于最后心跳+上限停止，额外尾期为零 | R10 |
```

### NFR-037
- Source lines: 925-925
- SHA-256 of exact extracted text: `e328da69bda24b532d82ec38048293d22f945c62e47d99ab8f890bed78b11a08`
```text
| NFR-037 | 全自托管 direct/tunnel/multi_hop，无计费/份额时正常运行；账号封禁、主套餐到期、运行授权/自治截止仍停止；含平台节点的路径无此豁免 | R11 |
```

### NFR-038
- Source lines: 926-926
- SHA-256 of exact extracted text: `6cd17745dee52062defe74d670e8d31bfdc30398b9f5fc3da854ff970826a70e`
```text
| NFR-038 | 当前包可定位附录C文件并识别本版本实际评审状态；未通过/待评审均不得通过require-frozen；实际冻结后须有该版本通过记录和证据，缺一拒绝。 | R12 |
```

### NFR-039
- Source lines: 932-932
- SHA-256 of exact extracted text: `13e4519ead683952b166e0e30da38978765b1c373c7f5ee0c31b2955ae659110`
```text
| NFR-039 | q=T−60、ttl=300、两版预发且运行/份额有效；准确时钟在T切换无固定10秒空窗。误差区间≤10秒时保护带内暂停写入但保留会话，≤12秒恢复；缺新版仍只申请200毫秒，自治硬截止不宽限 | F01 |
```

### NFR-040
- Source lines: 933-933
- SHA-256 of exact extracted text: `fe347f80568becf04a0e2f0fc94481ea18f6294916a5a1d76ea304a736635a28`
```text
| NFR-040 | 同node/boot份额50→60→40及延长所有权：prepared不可消费，旧续租权关闭不使旧门控失效，CAS后只有一个活跃修订；丢回执/重投仍只占max。连接降额排空前差额不可重发；跨node/boot和旧签发方未关闭时CAS必须拒绝 | F02 |
```

### NFR-041
- Source lines: 934-934
- SHA-256 of exact extracted text: `fad242d423b7ced87d378fa8a9a1d39c4a122e2c4730689d08122aa10710614e`
```text
| NFR-041 | 原委托100、旧在外A=30、未签发R=70；恢复时运行授权剩余<5分钟，对账持续10～30分钟，后继epoch按原R续签，旧授权消费不重计。轮转事务前后崩溃/重复transfer/双后继均不得复制R；原硬截止不延长 | F03 |
```

### NFR-042
- Source lines: 935-935
- SHA-256 of exact extracted text: `9b8246450769bc23ed73ec6b710c043a073cb7a4744e9135a5eeb56ebc73c9ee`
```text
| NFR-042 | 首个执行点actual尚未Commit、desired启用且Prepare成功时可取得bootstrap并Commit；先占后用。失败/取消/120秒超时撤销许可，但只按B.3安全终结回收，Commit未知时持续占用；详见NFR-045。本次apply引用撤销不影响其他规则；迟到Commit/重复申请不二次发放 | F04 |
```

### NFR-043
- Source lines: 936-936
- SHA-256 of exact extracted text: `7536bb35f9ebaaac3562aac58caa7a7644dedac7e645dca1492000d470c497d4`
```text
| NFR-043 | 旧来源B=0/F=80/L=80、新来源B=50/F=0时A_old=0/A_new=50，允许只向新来源委托50；结算旧60后新源不变。覆盖多来源、暂停/过期、新月、风险补偿，逐来源不超发，L不得转给新源 | F05 |
```

### NFR-044
- Source lines: 942-942
- SHA-256 of exact extracted text: `50fd936e6804a9d455ccbf0befda3b66765501bc6ce94bc0a105f27fd70fea61`
```text
| NFR-044 | 同租户/规则/执行点/组政策不变，将路径系数1改为3，旧100原始字节授权不能给新路径使用；新授权必须预留300计费字节。保留旧TCP/UDP会话、建新会话、迟到授权/重复提交/续签及回滚均按快照隔离；同系数不同路径修订也不得混用。再跨政策T，旧路径与新路径各用各自新政策快照；grant/batch/final系数篡改必须拒绝或隔离，逐来源占用守恒且最终结算一次 | G01 |
```

### NFR-045
- Source lines: 943-943
- SHA-256 of exact extracted text: `26ba998ebf6784dd80d4b653c05409b6da100788ed7b8dbe66c9b444d3a6f7e9`
```text
| NFR-045 | 许可120秒截止、节点119秒Commit、激活回执丢失；131秒控制端超时/取消/重启仍占用，不向其他节点重发。运行数据可有效至389秒且可能续租时，120秒许可到期不能证明安全终结；按所有可能签发及安全余量验证释放界。覆盖许可未送达、Abort/Commit两种先后、重复/迟到回执、新boot/节点失联、关闭续租失败、连接未清空、共享引用与更高修订重试；未知持续占用，只有B.3可靠证据可回收 | G02 |
```

### ENGR-001
- Source lines: 968-968
- SHA-256 of exact extracted text: `587235e6513596bb6bae30a3899c8b50bad3ac2de351798315a5421866ab1983`
```text
| ENGR-001 | Go、Node.js、全部依赖与容器镜像锁定版本，禁止使用浮动 `latest`；实现前提交工程版本基线。 |
```

### ENGR-002
- Source lines: 969-969
- SHA-256 of exact extracted text: `62a3480dfab865170b24970dec321e8a70460e3ea998ffdcb55100481e5f5cf8`
```text
| ENGR-002 | 闭源商业发布，只交付二进制与容器镜像；第三方依赖只允许 MIT / BSD / Apache-2.0 / ISC 等宽松许可，**禁止** GPL / AGPL；CI 自动检查许可证；发布物附版本清单、校验值、许可证清单与 SBOM。 |
```

### ENGR-003
- Source lines: 970-970
- SHA-256 of exact extracted text: `46fe325e2e9fbc9d6be30df658c4f09da9921d1fc89f800f6443d5eb2e576d02`
```text
| ENGR-003 | CI 使用 GitHub Actions，覆盖静态检查、构建、测试与制品生成。 |
```

### ENGR-004
- Source lines: 971-971
- SHA-256 of exact extracted text: `633c0f7d3d90a3017f7cfe514ee729b7cd5beddab7c6564e797fcb735f86c682`
```text
| ENGR-004 | 实现前须将数据库、OpenAPI、Protobuf、运行配置与 VLink 编码契约作为正式文件交付，逐条映射本文需求及验收编号并评审锁定；尚未交付时不得把历史附件编号当作契约已存在。V1.11 自身的需求语义以正文和附录 B 为准。 |
```

### ENGR-005
- Source lines: 972-972
- SHA-256 of exact extracted text: `86e2e4f6d608b7238545ab61abed1f5b6937622cc0f3d839ac74b4f51c1c9805`
```text
| ENGR-005 | 转发引擎在 CI 中执行网络命名空间拓扑的性能与泄漏回归。 |
```

### ENGR-006
- Source lines: 973-973
- SHA-256 of exact extracted text: `34781b68487fc2ada96e794b6279b35a2824261b1ee15f4c4cb2098596c36800`
```text
| ENGR-006 | 实现前建TLA+或确定性模拟器，覆盖既定故障及两两组合；验证逐来源F_s≤B_s+L_s、增量≤A_s、份额链交接max(a,b)占用且活跃代次唯一、账本转接R只消费一次、持久化消费幂等及未知上界不无资金释放、旧代次不复活。必须覆盖NFR-027～NFR-045，包括不可变计费快照隔离、Abort/Commit竞争及节点已激活但控制端未知时的保守占用。随包revision_scenarios.py仅验证本轮有限条款路径，不是完整模型或实现状态机一一对应证明。 |
```

## Supplemental source spans

### SUP-FMT-001
- Source: §2.3; lines 174-189
- Source-span SHA-256: `6d64d8e886d609a7b1a8aeb56aeba50a3b345d8a829c8cf40891bcdbb568024b`
- Coverage: §2.3 naming and format rules.

### SUP-STACK-001
- Source: §3.3; lines 225-248
- Source-span SHA-256: `00975a3260d1e311f7d7d8e22450d8f60831b5307e9bf6d2d08a8708e6c698ac`
- Coverage: §3.3 technology stack, persistence and deployment constraints.

### SUP-PLAT-001
- Source: §3.4; lines 250-252
- Source-span SHA-256: `ed9dd69440cfea0adc6fcaef86e22d6db09b6a48a164129703a37b83f8223e60`
- Coverage: §3.4 platform and browser support.

### SUP-IFACE-001
- Source: §8.2; lines 451-471
- Source-span SHA-256: `68c734ca322bb8d7fd502474d34a936cd11b59bf24d889d1ae508871bd1a3898`
- Coverage: §8.2 Engine process-local interface signature and semantics.

### SUP-AGENT-001
- Source: §9.1; lines 573-575
- Source-span SHA-256: `3d6de7732712a8a83dd9cc3fbefc2781906b8f947c4429d0fa1df875dfc9eb94`
- Coverage: §9.1 enroll/run/status/drain command surface.

### SUP-PAGES-001
- Source: §11.1; lines 743-753
- Source-span SHA-256: `784cc09c559724441e7dc191315a2a0b85b59ba932df735cd26721abaa1ee2f5`
- Coverage: §11.1 user and administrator page structures.

### SUP-RECOVERY-001
- Source: §12.4; lines 810-823
- Source-span SHA-256: `675022583efcd4ccbc365fdedcc60e232b9b258a2dbd24db161eac996eed4a11`
- Coverage: §12.4 six-step recovery flow.

### SUP-SCALE-001
- Source: §13.1; lines 829-840
- Source-span SHA-256: `c2e226f385fb482da71b2f922adff5fc1090bb8bedb733e3e732dbd509837056`
- Coverage: §13.1 scale baseline.

### SUP-TEST-001
- Source: §13.2; lines 842-850
- Source-span SHA-256: `1f5a7d58052d08bed39a9e692fbc55ea40196b51a49dc55f3666872bef9034fa`
- Coverage: §13.2 performance test environment and measurement conditions.

### SUP-METHOD-001
- Source: §13.3-13.5; lines 858-906
- Source-span SHA-256: `1e7d57ec09981428b0bfcfa58a4de27e033f96f01ef60fb9f639191875b4661b`
- Coverage: §13.3-13.5 stability duration, test methods and NFR-025 scenario evidence.

### SUP-STRUCT-001
- Source: §14.1; lines 947-962
- Source-span SHA-256: `647eead8dd572a1247062022b923b186c85de90f2dad66d58bfb681d4fab3186`
- Coverage: §14.1 repository structure.

### SUP-TOOLS-001
- Source: §14.3/CR-001; lines 975-983
- Source-span SHA-256: `839581bf683836a7d3991a45c5d55ccc01b34876854ca9f0f151416acd3a6ae5`
- Coverage: §14.3 test tools, with Vegeta replacing k6 per CR-001.

### SUP-MILE-001
- Source: §14.4; lines 985-999
- Source-span SHA-256: `e17e971fde5bbd528fe2ae48d93b7c6c88f787a59a494f471c5c2573591b900c`
- Coverage: §14.4 engine-first M0-M6 sequence and exit criteria.

### SUP-ARTIFACT-001
- Source: §14.5; lines 1001-1003
- Source-span SHA-256: `5da2c9094299d8013af12f39bb01fd53e8e9949cedb8ae5a204ab86e8179e271`
- Coverage: §14.5 required delivery artifacts.

### SUP-PARAM-001
- Source: Appendix A; lines 1037-1096
- Source-span SHA-256: `8a6e36df63a7e2f59db1fbdacfee3634d848ffa13e8945693a54af2251f16218`
- Coverage: Appendix A defaults and adjustable parameter ranges.

### SUP-B1-001
- Source: Appendix B.1; lines 1102-1108
- Source-span SHA-256: `4ff17052402bba490b2444db399182cb51089ce391ccef77a90586e37aabc5ad`
- Coverage: Appendix B.1 relative lease and absolute boundary rules.

### SUP-B2-001
- Source: Appendix B.2; lines 1110-1119
- Source-span SHA-256: `f892be92ec9b8cdc8f11cd6b654cb118b63de49f114f7b9bc5e11ea349f5b3a0`
- Coverage: Appendix B.2 sources, rounding and unknown-liability rules.

### SUP-B3-001
- Source: Appendix B.3; lines 1121-1132
- Source-span SHA-256: `a4b3e1073518aaece928dd2e24e5686d229e5657e6a1db0eda7653a4aac754b2`
- Coverage: Appendix B.3 share-chain and bootstrap activation safety.

### SUP-B4-001
- Source: Appendix B.4; lines 1134-1140
- Source-span SHA-256: `5a92a58ab2f4470e930abfd9f6798746283313a45063c1ef144dc453d199e45e`
- Coverage: Appendix B.4 ledger rotation and continuous signing.

### SUP-B5-001
- Source: Appendix B.5; lines 1142-1149
- Source-span SHA-256: `530fcbe66c67cc464a9e326ff10da9cc2326f61505f8b4e0a0ae4a4324301246`
- Coverage: Appendix B.5 immutable billing snapshots and concurrent configurations.

## Coverage audit

- Numbered table rows were selected only when a cell exactly equals the requirement ID; revision-history references and parameter-table references were excluded.
- 250 table records include AGT IDs in their second column; seven outside-table records are captured from their own labeled paragraph and associated table/list.
- Each numbered ID is unique and its source snippet is independently hashed.
- Supplemental IDs cover formats/terms, stack/platforms, interface, test methods, NFR-025, repository structure, tools, engine-first order, artifacts, Appendix A and B.1-B.5.
- Traceability for all 277 checkable items remains TBD / Pending. No implementation or test pass is claimed.
- Definition of Done requires same-commit local Docker and GitHub Actions evidence.

