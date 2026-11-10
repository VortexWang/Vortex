# 06-07 recovery HTTP / durable advance development evidence

基线 c17d1196。该单元增加已有Node、Hub同库和冷恢复的安全业务阶段读取，以及仅平台管理员可请求的同库恢复推进意图。原批准、boot/epoch/backend与原始证明不作为公网输入。推进任务精确绑定原批准、当前管理员和幂等键，调用既有有界协调器；不产生新批准或人工成功回执。

真实PostgreSQL + SQLite FULL场景串联 Bearer HTTP请求、持久化消费者、Hub manifest核验、Node实际围栏/新根安装回执和PG admission。等待阶段保持pending，unknown停放，只有真实证据齐备才成功；完成仍保持原资金占用。另覆盖8并发请求去重、8消费者租约争用、租约重取、当前角色撤销、tenant隔离、平台审计失败与队列/领取/结果回滚、非法公网内部字段、Node/cold安全投影。

保留的失败与修复：
- focused-native-race.log：新增普通用户测试未插入PG users身份，令牌签发正确拒绝；只补测试数据，focused-native-corrected-race.log通过。
- affected-native-race.log：8消费者重取场景检出真实SQLSTATE40P01。领取tenant→task，而结果task→audit/tenant形成逆序；结果事务加tenant优先锁后，同一场景在affected-native-corrected-race.log通过。affected-linux-race.log为修复前一次通过结果，不作为最终代码证据。
- initial-regressions-native-race.log：新增HTTP测试前的既有受影响回归通过。

sqlc-generation.json记录两次隔离生成，33输出逐文件相同且匹配工作区。cross-builds.json为Backend/Hub的Linux amd64/arm64构建成功，不代表目标CPU执行。full-backend-{native,linux}-race.log是最终完整后端竞态检查。源码快照从完整测试期间记录至结束核对，未声称测试开始前就已采集。所有结果都是当前开发工作区证据；干净源码验收和相同SHA GitHub Actions配对尚未完成。
