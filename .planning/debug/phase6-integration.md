---
status: investigating
trigger: 用户要求先收敛F-10/F-11，再整合Phase6到主线
created: 2026-11-09
updated: 2026-11-09
workflow: gsd-debug
execution: inline
---

## Current Focus

hypothesis: F-11与Phase6已有的夹具初始化/运行截止分离修复相关；F-10需区分新grant的保守生效时间和事务排队。
next_action: F-10/F-11最小修复已通过native及限两核Linux重复race；合并Phase6并运行完整入口。

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

root_cause: pending
fix: pending
verification: pending

- F-10 deterministic RED: success checkpoint缺少pre-exhaustion通知。补充普通/批量结算成功后的coalesced scope wake；return、cancelled checkpoint、foreign revision不得唤醒。原生相关race三轮52.873s，限两核Linux稳定源码三个场景三轮67.047s。最初Linux在源码编译期间修改，失败保留，不能作为修复后判定；随后固定源码重跑通过。
- F-11沿用a2097430测试修复，原生五轮21.530s，Linux包含在67.047s。初始化与证明context分离，真实5秒运行和200ms admission不变。最终完整入口/Actions尚待集成。
