---
status: investigating
trigger: 用户要求先收敛F-10/F-11，再整合Phase6到主线
created: 2026-11-09
updated: 2026-11-09
workflow: gsd-debug
execution: inline
---

## Current Focus

hypothesis: F-10成功消费缺少提前唤醒，F-11夹具混用初始化和实际运行预算；整合额外暴露通道权限边界与旧夹具缺失明确授权。
next_action: 已提交真实Phase6合并fb3a9786；完成干净Linux完整回归，补证据并提交精确CR-012审阅，批准后运行最终同SHA入口。

## Symptoms

expected: 连续TCP/UDP在真实耐久授权内续资并逐来源守恒；区域故障切换保留未知负债且在独立5秒运行预算内完成。
actual: Actions连续授权超过200ms后TCP收到RST，UDP收到非预期载荷；本机恢复入口silent-established的查询因5秒总context超时失败。
repro: Actions42606221450/42608942871；本机干净168be8a5完整Phase5入口。
timeline: Phase4合并后最终审查；Phase6已在独立分支完成验收，但尚未整合。

## Evidence

- 原Phase6工作区为codex/phase6-backend@c85ed753且有用户未提交修改；持续保留，不checkout或stash。
- Phase6已有a2097430，保留5秒真实运行预算，将migration/TLS初始化与最终耐久证明使用独立context。
- Phase4实验CreditDemand使用tag100；Phase6新增生产命令使用19..24等。整合应同时保留已批准CR-010/011和Phase6 CR-008/009，不回退契约或移除任一范围。

## Eliminated

- 延长生产200ms或忽略unknown不属于授权修复。
- 删除Phase4新增连续流量测试来适配Phase6不是回归收敛。

## Resolution

root_cause: 成功耐久消费未唤醒续资；恢复夹具将初始化时间算入5秒运行预算。两分支的ACK接收路径与角色隔离必须同时保留；旧恢复/到期夹具缺少生产安全边界要求的明确后继根和真实后端usage确认。
fix: 已原子提交F-10/F-11及整合修复。保持200ms、5秒实际运行、source-owner、FULL持久化和unknown负债边界。
verification: corrected full native root/backend race PASS；双Protobuf/双sqlc/六Linux构建与契约负向PASS；干净Linux与最终冻结后的Docker/Actions待完成。

- F-10 deterministic RED: success checkpoint缺少pre-exhaustion通知。补充普通/批量结算成功后的coalesced scope wake；return、cancelled checkpoint、foreign revision不得唤醒。原生相关race三轮52.873s，限两核Linux稳定源码三个场景三轮67.047s。最初Linux在源码编译期间修改，失败保留，不能作为修复后判定；随后固定源码重跑通过。
- F-11沿用a2097430测试修复，原生五轮21.530s，Linux包含在67.047s。初始化与证明context分离，真实5秒运行和200ms admission不变。最终完整入口/Actions尚待集成。

- 合并发现并用真实mTLS RED/GREEN修复receive侧ACK绕过ControlOnly；known/unknown outbox、hook与未知负债均核验。CLI/客户端拒绝实验需求与direct backend混用。
- 有限DemandLimit错误计算同ID重放，导致提前结束控制连接；distinct-ID fixture RED后修复，32轮原授权期限测试PASS31.196s。
- 原到期闭环夹具缺少usage后端确认；记录region真实错误后补实际Hub/backend mTLS，三轮PASS23.873s，不放宽生产路径。
- 原Phase5恢复夹具未供Phase6新增的原source owner明确后继根；补actual persisted allocation/hash/三根相邻revision/epoch，全部五场景PASS8.702s，保留原R与512unknown断言。
- CR012候选精确描述符并集105=union(main69,phase6100)，历史CR009篡改测试与forward篡改测试均PASS。完整native修正root及backend race均PASS；service569.990s。Linux干净fb3a9786启用真实4MiB满盘夹具，root/Engine全量race通过（Agent253.534/Store258.953/Phase5 process112.495s），源码前后干净；隔离Linux后端全量race仍在运行。
