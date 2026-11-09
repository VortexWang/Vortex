# 06-07 Task1：令牌持久化单元

PG hash-only opaque access/refresh；family固定硬截止，refresh不滑动延长；可信Issue不接受公开角色声明。每次access读取当前user/platform role/state及tenant state。refresh family行锁串行轮换；旧refresh重用撤销全family；logout独立撤销。签发、轮换和重用撤销与共享AuditWriter同事务，失败全部回滚。

通过：complete-native-race.log(1.866s)、complete-linux-race.log(1.156s)，六项真实PGauth测试；final-native/linux-race.log包含既有policy/repository回归。新增平台/Issue负例是后续补充，最终complete日志覆盖。sqlc两次独立生成与本地24输出完全一致；Backend Linux双架构构建通过。

故障证据：initial-contract-red.log是未实现导致编译失败；initial-implementation.log是sqlc CASE类型推断导致编译失败，均不算行为RED。显式移除family revocation后reuse-revocation-semantic-red.log实际检出旧access仍可用；恢复原源码后最终通过。同一次开发实验与首个Linux编译重叠，overlapped-mutation-linux-failed.log编入了故障变体，不能作为生产回归结果，保留以说明原因。后续Linux在恢复且稳定的源码上通过。

迁移9仍候选，domain_events仅预建表、SSE未实现。无publicHTTP登录/认证、完整REST/SSE或06-07完成声明；Phase6仍9/11。开发证据不代替最终同SHA Docker/Actions配对。
