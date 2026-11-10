# 06-07 干净源码候选验收

精确源提交 `97966a3a70bff79afd43078ceadf211fe877a6d5`，独立克隆与全新Compose项目/PG；正常producer退出0，源树前后干净。普通/race测试分别482个测试及子场景通过，未跳过任何实际测试；六个无测试文件的包由Go产生package-level记录并在校验器中明确列出。

| 检查 | 实际结果 |
| --- | --- |
| 完整Linux arm64 backend normal | 通过，service227.988s |
| 完整Linux arm64 backend race | 通过，service383.572s |
| freshPG migration9、sqlc隔离生成2次 | 通过，33输出与提交逐文件一致 |
| Backend/Hub/Node Linux amd64/arm64构建 | 六项通过；元数据均绑定同SHA且vcs.modified=false |
| 干净克隆OpenAPI/语义负向检查 | 45操作lint、5组负向通过 |
| 干净克隆依赖/许可 | 38业务编译模块、42sqlc链接模块及既有精确例外通过 |
| 原始证据与精确候选 | clean-docker-validation.json验证实际测试/元数据；99规范文件与测试源完全一致 |
| 证据/规范负向检查 | 六类坏证据、六类坏候选拒绝；final-semantic-gates.log |

CR-009精确JSON摘要：`ed7ca2f0cf68f46d29984b7ddafbc13fc8de4ad171cd3378b0b9dda8e45bdfe0`。owner approval仍false，不创建批准或冻结，不生成06-07 SUMMARY。开发native完整backend race399.303s及先前Linux开发race405.885s见task1-recovery-http；与本目录干净源码证据分开。

失败保留：独立环境最初固定端口16390被占用，Docker未启动测试；runner-port-collision.log。改用空闲端口后完整检查通过。公共契约checker首次解析历史中文Git路径的quoted输出失败，改用NUL路径后通过；原日志位于task1-public-contract/exact-candidate.log。真实恢复锁反序/缺用户fixture失败分见task1-recovery-http，不覆盖原记录。

本机Docker实际架构arm64、VM资源/镜像见host.txt和docker-host.json。不是nativeamd64、Ubuntu24.04资格、性能、完整模型、物理断电或外部Cloudflare真实CRUD验收。最终同SHA Actions及完整跨域验收归06-11。原通用check.sh尚未支持through06-07，本次使用明确有界的rest_container_check.sh，不将旧入口写为通过。历史VLink治理hash差异保持既有状态，本CR未修改/重新放行该规范。
