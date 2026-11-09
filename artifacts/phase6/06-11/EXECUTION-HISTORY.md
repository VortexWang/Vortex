# 06-11 验收尝试记录

最终运行目标：`65a5335894732b7d632e870acc3d8283be224cd3`，Actions `42572674719`。最终状态只由该提交的三端原始结果与配对门禁决定。

| Source / attempt | Result | 说明 |
| --- | --- | --- |
| development before bounded drain | FAIL | 原worker每任务等待100ms；60秒仅收敛300规则/600成员，原始RED保留在task1-reconciliation。 |
| development after drain | finite qualification PASS | native/Linux race35.674/21.082秒；共享开发DB的其他回归失败仍保留，独立新DB相关回归通过；不能作为clean-source验收。 |
| efdb89de | FAIL | Compose只挂载输出目录但未导出容器PHASE6_EVIDENCE_DIR，未开始Go测试；84f9修复。 |
| 84f9cc86 | FAIL | 完整普通套件通过，race编译被宿主OOM杀死；不是race通过。0ca9限制编译并发/GOMAXPROCS。 |
| b955f235 attempt1 | FAIL | 普通套件末尾链接被OOM杀死；保留kernel证据。仅停止本轮创建的测试项目以释放内存，未全局prune。 |
| b955f235 retry2 | superseded / interrupted | 普通491测试/子场景通过，对账16.067秒；race对账21秒附近完成，但完整race及后续未完成。审查发现凭据guard退出码被条件shell吞掉，3fb8修复后中断旧轮次，不能算最终通过。Actions42545713479已取消。 |
| 3fb8e25a attempt1 | FAIL | Docker预定义网络地址池耗尽，尚未开始测试。只移除本轮三个已停止项目的容器/网络，保留数据卷和其他项目。 |
| 3fb8e25a retry2 | FAIL | 完整backend/root/Engine normal/race与fault通过；固定Go镜像无Ruby，契约检查失败，Cloudflare未运行。Actions汇总/上传因root-owned0600报告失败，CI运行结果无法完整取回，不算通过。 |
| d852c99d | superseded | evidence owner修复经真实Linux volume验证；Actions42550264137在Ruby缺失被发现后取消。 |
| 49b64e03 | FAIL | 本机完整backend/root/Engine normal/race、fault及生成通过；私有policy相对路径导致外部检查失败，绝对路径续跑真实CRUD通过，但Python3.9不能解析Go的8位小数RFC3339Nano。两架构Actions普通套件通过，race均在真实tracer遇40P01死锁，1000对账60秒仅973/971条，均保留失败原始数据。 |
| 4420e625 | superseded | Lock-order/RFC3339Nano fixes; development race qualification27.084s. Query/abort lease path refined in30653e9b; Actions42554341197 canceled before acceptance, local full suite not run. |
| 30653e9b attempt1 | FAIL before Go | Local checkout used host-only Git alternate objects; container could not resolve HEAD. No Go test ran. |
| 30653e9b retry2 | FAIL | Backend normal/race492 and1000/2000 in13.709/13.054s, root normal655 and Engine normal249 pass. Root race debt proof used the fixture-wide5s context after successful failover and timed out. Complete original failed folder retained; Cloudflare not run. Separate fixture/runtime/proof deadlines while retaining5s runtime bound; fresh three-host source required. |
| a2097430 | superseded/interrupted | Fixture preparation/runtime/proof contexts isolated; native5/Linux3 focused race pass. New complete Docker + native dual Actions42557418686;30653e9b CI remains historical separate evidence. |
| f54c8017 | Docker PASS; Actions pending | Complete Docker backend502/root655/Engine249 normal+race, fault, generators, exact freeze/licensing and realCloudflarePASS.1000/2000 normal8.999890s/race13.208725s. Native dualActions42560179302 still running; no pair yet. |
| f54c8017 | FAIL | Docker与Actions42560179302 arm64完整通过含真实Cloudflare；amd64 backend502normal/race与严格1000门槛通过，rootrace33规则续资触发隐藏重连而失败；EngineRace/fault/Cloudflare未运行。原始失败保留，不形成三端配对。 |
| f466bfab | running | 续资每轮4条轮转；旧源码RED、新native/Linux重复3race通过；新cleanDocker及Actions42565204602双架构完整运行，最终状态待原始证据。 |
| f466bfab Docker attempt1 | FAIL | 宿主磁盘100%，Docker daemon退出，unexpected EOF/exit128；Go测试尚未开始，Cloudflare未运行。清理可重建Darwin Go编译缓存释放15GiB，保留原始证据，完整重试同SHA。 |
| f466bfab Docker attempt2 | FAIL | Backend normal/race502、root656、Engine249、对账8.846/12.626秒、ledger/share fault全通过；固定Protobuf v36.2 GitHub下载连续504，未进入双生成/Cloudflare，producer exit1。原始证据保留于artifacts/phase6/06-11/final-f466bfab/docker-failed-generator-504，不算通过。 |
| f466bfab Actionsattempt1 | FAIL | arm64完整/真实CloudflarePASS；amd64恢复续资strictAttempts2失败{3 2 0 0}，原始race498PASS/1FAIL。诊断确认快照时序竞态；不算配对。 |
| f466bfab Actionsattempt2 / Dockerattempt3 | FAIL | 分别sqlc下载与Protobuf宿主预检504，未开始Go套件或Cloudflare，保留原始记录。 |
| 65a53358 | running | consistent snapshot then clock observer + bounded exact-asset official download fallback；Docker与Actions42572674719双架构完整运行。 |
| 65a53358 final | PASS | Docker与Actions42572674719原生amd64/arm64完整通过，含真实Cloudflare CRUD；三端递归hash/语义和两公开pair入口通过。快照观察修复与官方同资产下载入口未改变原权利、截止、watchdog或冻结契约。 |

`producer-secret-guard/negative.json` 使用合成字节验证当前脚本的真实 `&&/||` 上下文：泄漏返回1，脱敏返回0。真实token不写Git、日志或此记录。

所有取消、环境资源失败、部分完成和开发测试均与最终验收分开记录。历史Phase3 VLink review-hash差异保留原状，不当作Phase6五个相关契约冻结的通过证据，也不授予新的历史批准。

49b本机的 `docker-attempt1/` 是原始失败producer；`docker-external-continuation/` 保留所有原始运行字节及hash、第一次外部失败、只重试真实CRUD的provenance和Python3.9汇总失败，均不是最终验收通过。Actions的两个failed目录是未修改的下载产物。权限修复使失败原始数据现可完整上传并审查。

后续修复：task-only lease先提交，再以tenant→task顺序复核当前租约/授权并提交running，任何superseded或失效租约不能触达transport；receipt/deployment统一tenant→head顺序。确定性PG锁测试RED55P03、修复GREEN。Run启动及每秒恢复扫描替代每个member重复扫描，Once显式恢复语义不变。60秒门槛没有放宽。RFC3339Nano以整数纳秒解析0..9位小数，跨秒/无效格式负向测试通过；本机policy绝对路径预检前置。SQL、schema、冻结契约和依赖均不变，需要修复提交的新完整三端结果。

30653e9b Actions42554576162: arm64 fullruntime+CloudflarePASS, amd64 tracerFAIL with port-operation40P01; no pair. Root cause extends tenant FK lock order to event writes in control/query/abort/release/projection. Six old-source deterministicRED55P03, newGREEN with unchanged strict timing gates; details inworker-event-lock-order.

