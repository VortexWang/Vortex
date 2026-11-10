---
status: verifying
trigger: 用户要求先收敛F-10/F-11，再整合Phase6到主线
created: 2026-11-09
updated: 2026-11-10
workflow: gsd-debug
execution: inline
---

## Current Focus

hypothesis: F-10成功消费缺少提前唤醒，F-11夹具混用初始化和实际运行预算；整合额外暴露通道权限边界与旧夹具缺失明确授权。
next_action: 修复并发耐久续额在 SQLite 单写连接上的 200ms admission 超时；保持安全边界和独立流授权不变，完成同 SHA Docker/Actions 验收后再整合主线。

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
verification: corrected full native root/backend及干净fb3a9786 Linux root/Engine/backend race PASS；双Protobuf/双sqlc/六Linux构建与契约负向PASS；CR012精确批准及冻结门禁通过；最终同SHA Docker/Actions待完成。

- F-10 deterministic RED: success checkpoint缺少pre-exhaustion通知。补充普通/批量结算成功后的coalesced scope wake；return、cancelled checkpoint、foreign revision不得唤醒。原生相关race三轮52.873s，限两核Linux稳定源码三个场景三轮67.047s。最初Linux在源码编译期间修改，失败保留，不能作为修复后判定；随后固定源码重跑通过。
- F-11沿用a2097430测试修复，原生五轮21.530s，Linux包含在67.047s。初始化与证明context分离，真实5秒运行和200ms admission不变。最终完整入口/Actions尚待集成。

- 合并发现并用真实mTLS RED/GREEN修复receive侧ACK绕过ControlOnly；known/unknown outbox、hook与未知负债均核验。CLI/客户端拒绝实验需求与direct backend混用。
- 有限DemandLimit错误计算同ID重放，导致提前结束控制连接；distinct-ID fixture RED后修复，32轮原授权期限测试PASS31.196s。
- 原到期闭环夹具缺少usage后端确认；记录region真实错误后补实际Hub/backend mTLS，三轮PASS23.873s，不放宽生产路径。
- 原Phase5恢复夹具未供Phase6新增的原source owner明确后继根；补actual persisted allocation/hash/三根相邻revision/epoch，全部五场景PASS8.702s，保留原R与512unknown断言。
- CR012候选精确描述符并集105=union(main69,phase6100)，历史CR009篡改测试与forward篡改测试均PASS。完整native修正root及backend race均PASS；service569.990s。Linux干净fb3a9786启用真实4MiB满盘夹具，root/Engine全量race通过（Agent253.534/Store258.953/Phase5 process112.495s），隔离Linux后端全量race通过（service550.929s），源码前后干净。15项完整证据及哈希见validation-manifest.json。原用户工作区HEAD/tracked diff再次一致；最终status新增外部未跟踪Phase7目录，未由本轮创建或修改，保持原样。

- 用户明确批准CR-012，精确proposal ecc3cbbdbfcb6e0cbb4144b44fdfe70c163f7edbf722fe4bdc04d37d7dd1f547。仅control catalog候选改冻结、追加精确批准记录及current governance摘要，候选规范和历史批准字节未改。原CR009快照门禁、全局冻结、两基线并集和批准后负向检查通过。

- 779029b2最终Actions暴露checkpoint等待期间AfterFunc延迟的取消竞态：dispatch42623942243在cancel-member错误返回成功。新增delayed-cancel-member以FIFO follower延迟传播，确定性RED0.653s；取得事务后及提交前同步校验原member contexts，native race20轮38.400s通过，原committed/reserved/unknown/outbox及reopen/replay守恒断言保留。CR012精确门禁仍通过，规范/提案/批准字节未变。
- PR42623892470同时在多包并行wave1中出现持续授权及恢复RST，保留FAIL。将wave1/wave2/full gate所有root多包入口统一-p1，与既有authorization策略一致；job预算60分钟覆盖累积串行全套，不改变测试内200ms/5秒deadline。779029b2的最终证据不能替代修正后新提交配对。

- 679b5501最终未通过：Docker backend normal/race各502项通过，但root引入5个Phase4 opt-in skip被严格门禁拒绝；Actions42625295380两原生架构root-race持续TCP/UDP超200ms失败；Phase5 dispatch42625300678与PR42625221806 attempt2同样失败。Phase1..4在679的PR结果通过。这些结果不能用于合并放行。
- 后续确定性RED：重复8次missing-authority wake发送同一pending请求8次（0.690s）；相同请求的sequence FULL提交、Hub重复处理和Agent重复安装与真实授权竞争。另发现25ms窗口内丢弃最后一次成功消费wake。改为session-local有界重发间隔、重连立即按原ID重放、保留trailing wake；pending精确scope直接跳过重扫账本，事务内去重和权限校验保持不变。
- 初次仅重发节流仍在Linux双核race五轮中的一轮UDP失败，保留该FAIL；增加pending账本重扫规避后Linux双核race连续TCP/UDP三轮PASS48.802s。原生需求/身份/重放race三轮PASS47.675s、身份和重发race十轮PASS10.941s。开发快照不是最终same-SHA证据。
- af17f907 最终独立进程验收复现出真实并发回归：`TestDurableAuthorityProcessesConcurrentRenewal` 在 normal/race 均可能收到 `FrameRST` code 8；节点日志显示 16 条独立流各自执行 durable funding admission，SQLite 单写连接在并发 settlement/lease 校验下等待约 195ms，固定 200ms `missingAuthorityWait` 到期后安全拒绝并由 relay 发送 RST。扩大 deadline、忽略 unknown、删除测试或降低安全边界均不接受。当前尚未形成满足安全与性能双重约束的最小修复，故不合并。
- Phase6 root仍拒绝任意required skip。入口明确只排除TestRuntimeDurableD04Candidate（tools/phase4/d04_durable_scan.py独立参数矩阵）；每模式分别构建真实Node/TestHub二进制并要求四个进程功能测试通过。证据新增精确root-selection.json和scope/process缺失负向检查。原生四进程功能测试PASS48.822s，开发dirty快照不当正式验收。
- 扩大Python discover时历史CR009原始契约测试直接针对当前CR012组合规范运行而失败，精确current_contract_check.sh仍PASS；不改写历史批准规范或将此失败写成通过。新的最终证据门禁3项含scope篡改/required skip/process缺失负向PASS。

- 2026-11-10续跑：新增Store跨session funding admission有界合并、同事务authority只读复用、同方向idle tail原子换块、无竞争Agent直接路径和UDP indivisible-write水位修复。保留200ms及60s，取消/存储错误全回滚，不放宽unknown。新守恒、回滚、重启/速率、UDP waiter测试已针对性race通过；尚未通过最终真实续额进程。
- ACK排空调查：GgIpS3真实4096 round trips/12288000 bytes业务45.522s完成，8192 usage全部durable，60s时仅4433 hub_acked、3759 pending。首批直到10s发送，每条Agent ACK与Hub usage/origin单独FULL竞争；4096 replay rotation保持不变。
- 新交付：按SQLite插入位置分页，每轮最多32 known/typed unknown公平发送；commit后广播hint、50ms合并突发提示，10s恢复重放；cursor不是ACK。序列号同页耐久预留且崩溃不重用，MaxInt64不回绕。ACK相邻信封逐一replay验证后有界同事务提交，commit后hook；配置通道/unknown拒绝、EOF drain和重连重放均保留。Hub usage+journal+首次origin同一FULL事务，backend确认边界不变。
- 真实Linux失败证据保留：Luljiv round87 RST、H8xsh9 round131 RST、lFCBA5 round150 RST。单SQLite写连接竞争仍可耗尽200ms；NextSequenceRange/ACK纳入现有FIFO，checkpoint仅合并同秒需求估算样本（每条usage/residual仍独立）。这些失败不能用于放行。
- 本轮native针对性race：usage/config-only/replay/rotation/unknown Agent10.320s、Store4.242s；ACK群提交/来源原子/序列范围/公平分页 Agent9.475s、Store5.099s；checkpoint取消/需求窗口/跨session守恒Store7.674s。CR009历史保护及CR012精确并集门禁通过，描述符69/100/105未改变。真实进程、完整相关套件、same-SHA Docker/amd64/arm64 Actions仍待通过，不合并PR1。

- 最新真实Linux burst50试验AI7zqa/process-1600419660仍FAIL（round94，18.63s），未放宽任何预算。需求估算合并+ACK公平队列仅为待验证修复；不把改善路径当验收。新增普通ACK一次commit/混unknown全回滚/hook后置、序列重启范围、known/unknown公平append分页及新hint、origin原子失败/reopen等targeted race均通过，最新Store3.953s/Agent9.311s。完整Agent/Store/Hub race运行日志 /tmp/vortex-integration-unit-full-race.log，尚未结论。
- 下一步精确测量：最新RST前没有独立funding admission超时日志，需核对fundingBlockSession.Reserve无竞争快路径在ctx已取消但batch已返回时的Return分支、checkpoint的session锁等待与组内commit边界，不能继续仅凭RST假定为200ms数据库排队。PR1仍Draft@af17f907，无新提交/推送/主线合并。

- 最新突发交付样本失败后，补充同事务只读关系证明复用：只有funding group的authorityRows/source/share不变时缓存成功lineage/share验证；时间/session/rate/余额继续逐次检查。新增同事务过期拒绝/下一事务撤销拒绝测试。Linux容器vortex-admission-linux-lineage-proof，exec59096，证据路径由/tmp/vortex-admission-current-linux给出；未完成前不要重启同一试验。完整组件race exec74565运行中，源码曾追加只读证明优化，因此此套件只作开发回归，不当same-SHA最终证据。

- 只读关系证明复用后Linux process-3309838609仍round145 FAIL；小组8试验process-3969923421更早round27 FAIL，已撤回小组改动恢复64。新增失败/慢路径诊断，下一轮必须先量出session锁等待、提交赢过deadline时已commit返回的具体分支再改算法。Store完整race143.287s通过（开发源，Agent/Hub仍执行中exec74565）。proof/time/sample/cancel targeted race7.890s通过。

- 续跑完成上一轮完整 native component race：Store143.287s、Agent179.033s、Hub12.499s。仅开发快照，不作最终same-SHA证据。noautockpt独立拷贝FAIL process-55941604；合并checkpoint/admission队列FAIL process-4078415058；named-volume FAIL process-1433023404；每批5ms窗口FAIL process-4036329291。合并队列与5ms窗口均已撤回，正式FULL/auto-checkpoint/200ms不变。
- 会话锁诊断证实结算持有全会话mutex，反方向在进入Store前累积等待。将checkpoint等待期间的block标记为in-progress，保留inFlight/residual上界并释放全会话mutex；同block准入/其他结算等待该结果，独立方向可进入writer队列。外部SQLite writer阻塞的确定性测试验证无提前ACK、无旧residual签发、allocator可获取、取消后资金守恒，race三轮PASS2.503s；相关funding/fragment/UDP回归PASS7.795s。
- 此修复未关闭真实进程回归：named-volume process-2967487863 round64 FAIL，单funding commit209.491ms；独立auto-checkpoint=0诊断process-1077773606 round60 FAIL，单checkpoint commit298.873ms（work23.322ms）。禁用checkpoint不是修复，正式工作区保持默认自动checkpoint。三并行4KiB write+fsync探针各1500次，p99约8ms、max70.7ms；仅诊断，不能替代SQLite/系统验收。
- 下一步采集Linux fsync/fdatasync syscall时间定位commit暂停。PR1仍Draft/Open/MERGEABLE@af17f907，未新提交或合并；所有失败证据保留在/tmp/vortex-*-result.*，不作为放行依据。

- 当前完整native race已结束并通过：Store136.011s、Agent171.597s、Hub12.624s；源码覆盖本轮checkpoint锁范围修复。current_contract_check.sh再次通过CR009历史保护及CR012精确冻结并集105，git diff --check通过。原生进程opt-in不替代Linux真实四进程验收。
- strace6.13诊断process-2542338185：616次已结束同步调用中最慢fsync149.692ms，与checkpoint commit150.586ms对应；其后funding排队165.635ms、工作/提交22.319ms，外部Reserve总208.547ms拒绝并返还未用资金。strace改变时序且wrapper引入退出超时，仅作归因证据，不能作验收。当前仍未解决真实并发回归，PR1保持草稿。

- 续跑已提交并推送 b2c42581095440c4945f89c40e1c49bb5332cb7c，PR1仍Draft/Open，未合并。native component race Store136.011s/Agent171.597s/Hub12.624s为开发快照。干净b2c四真实进程 normal ConcurrentTCP/ConcurrentRenewal及race ConcurrentRenewal仍FAIL，证据 /tmp/vortex-process-b2c42581-root 指向clean clone/evidence。
- b2c Actions dispatch Phase6=42721349101，amd64 job129515809740在backend-normal恢复测试FAIL：expected两次连接实际三次，额外错误trusted time uncertain；arm64截至本记录仍运行。Phase5=42721354032已FAIL：所有Go测试通过，最终audit_recovery发现epoch快照被older-path场景覆盖。原始失败artifact保留 /tmp/vortex-b2c42581-actions-amd64-artifact 与 /tmp/vortex-b2c42581-phase5-artifact，不作为放行。
- 当前未提交修复：funding/checkpoint批次取得FIFO writer后才收集等待期间到达的成员，原ctx同步取消检查保留；delivery metadata用独立队列，不能与funding混用取消域。确定性RED旧队列拆16请求为2commit；新收集race通过，独立delivery取消不取消funding测试race三轮通过。checkpoint相关race三轮14.981s。
- 当前未提交交付窗口：每连接最多32（与delivery page一致）未确认身份，known/typed unknown独立；发送前claim，真实ACK事务及hook成功后释放hint，10s/重连从durable pending重放，不把cursor/window当ACK。窗口公平/unknown冻结/重放targeted race通过。
- 实测已确认SQLite分页旧查询按node主键扫描全部历史并临时排序。新增pending部分索引(node_id，隐含rowid顺序)，EXPLAIN确认rowid>? seek且无TEMP B-TREE，新索引测试race通过2.045s。索引后真实业务4096 roundtrips/12288000 bytes均完成约30.4s，但总60s ACK排空/恢复重放仍FAIL，不能当验收。
- 注意诊断证据：immutable=1忽略仍存在的WAL，不能用它统计运行失败时已提交总状态。最新改用modernc mode=ro读取copy中的db+wal。group-replay实际Agent8192 hub_acked、Hub8192 ledgered、backend仅1837；测试失败在runtime_process_test.go:401的重启后backend drain。之前直接immutable统计的pending数只反映checkpointed主文件，不能解释完整durable状态。
- 当前未提交Hub known usage有界群提交：≤32个相邻已到达known报告逐信封身份/sequence校验，保存独立payload/digest/首个provenance，同一FULL提交；后端每条digest都确认后再群提交receipts并ACK。typed unknown独立路径和未知冻结保留。fixture独立backend数据库实现同事务群提交；真实backend无group接口则逐条实际确认，不伪造结果。恢复ReplayPending按known≤32群确认；旧缺provenance仍pending。rollback/origin/reopen/receipt错误、wrong digest不产生部分receipt、command/EOF顺序/replay、unknown隔离targeted race通过。
- 时钟根因：UsageRelay在读CurrentAutonomyStatus之前采样，等待期间新backend heartbeat可能提交，导致把正常心跳误判future。状态读取后再采样可信时间，仍拒绝不可信/倒序，不延长deadline、不签发authority。新确定性竞态测试race通过，backend真实mTLS恢复定向三轮24.626s通过；一次错误数据库名的初始运行FAIL明确保留 /tmp/vortex-recovery-clock-native.log，修正URL结果 /tmp/vortex-recovery-clock-native-corrected.log。
- Phase5证据命名：普通epoch保留epoch，older-path改epoch-older-path；新增旧path4的6条revoked、新path5 epoch2的3条active、unknown512/floors独立审计；原epoch断言不放宽。两场景race3.148s通过，/tmp/vortex-epoch-separated指向证据。尚未完整Linux复跑审计；macOS Python mode=ro对新SQLite WAL快照打不开，不冒充通过，也未修改正式审计为immutable。
- 真实Linux新试验全部未放宽200ms/60s：window-checkpoint round213 RST FAIL33.65s；索引后delivery-index业务完成但ACK deadline；usage-group与group-replay业务约31.3s完成、Agent8192确认，重启backend drain在60s FAIL；window32业务32.068s、到49s全部ACK，但重启backend尚剩3644在60s FAIL。所有容器保留，/tmp/vortex-*-result指针保存原始证据。
- 最新假设：PendingBackendUsage为每个pending行额外执行UsageOrigin查询，恢复几千行形成N+1 SQL开销。已改同一快照LEFT JOIN读取provenance，历史缺失仍空/0，不改变权威；运行中的真实试验 exec5646，container vortex-admission-linux-replay-join，结果 /tmp/vortex-replay-join-result 指针，尚未结论。期间不要修改生产文件/重启同一实验。当前HEAD仍b2c，所有后续改动未提交，需完整review/test/contract以及same-SHA Docker+Actions双架构通过后才允许合并。

- 续跑补齐结果：b2c Phase6 arm64同时在持续UDP及并发续额FAIL；其原始证据保留。LEFT JOIN恢复试验仍60s backend drain FAIL；prepared usage试验在round181 RST FAIL28.28s，commit100.487ms+work38.770ms叠加后续funding排队，不作放行。
- 检查点热路径精简：父funding block完整身份/残量/hold状态校验后，同事务直接插入committed child reservation/hold和known outbox，减少新建后立即重读与更新；未知身份冲突、旧checkpoint精确重放及原子残量上界不变。新增固定SQL预编译只缓存statement，不缓存权限/余额。相关checkpoint/rollback/cancel/crash race三轮PASS17.130s。
- dirty开发快照Linux真实并发续额PASS50.732s（/tmp/vortex-checkpoint-direct-result.2ame7J），16流4096round trips/12288000bytes业务27.147s，包含全部ACK和重启后backend replay。将恢复观察COUNT改为等价EXISTS，零pending条件保持；完整四进程race PASS71.750s，证据由/tmp/vortex-process-direct-matrix指向。开发快照不充当最终same-SHA证据。
- Phase5分离快照在Linux原始audit_recovery.py开发组合审计通过：snapshots7/fault_cases11；混合旧Actions与新开发证据仅作诊断，不作为正式放行。时钟竞态/receipt群提交/来源索引/传输窗口针对性race均通过；current_contract_check再次PASS。完整native组件race /tmp/vortex-direct-component-race.log仍运行。
- 当前候选准备提交并重新产生干净同SHA Docker与Actions双原生架构证据；200ms/60s/5s、FULL/WAL及unknown冻结均未放宽。PR1仍未合并。

- 74c809f5已提交推送。完整native component race PASS：Store146.685s、Agent174.034s、Hub13.476s、fixture2.215s。Phase1/2/3/4 PR检查通过。干净同SHA本机Docker backend normal/race各502项PASS；但Phase5 dispatch42729068806增量授权FAIL，TCP/UDP TestRuntimeFundingBlockContinuousTraffic均200ms超时，原始日志和artifact保留 /tmp/vortex-74c809f5-phase5-failed.log 与 /tmp/vortex-74c809f5-phase5-authorization/。因此主动停止该已失败候选剩余本机runtime工作，result.json标记failed/Cloudflare not_run，不能用于放行。Phase6 dispatch42729061360仍在运行，需收取最终结果。
- 失败定位：小额grant碎片收集逐个FULL预扣、余额不足后又逐个FULL退回；在单次200ms内串行竞争多轮writer。新增ReserveRuntimeUsageFragments把≤64个独立grant child及各自真实速率/余额/权限校验置于一个有界事务，完整收集后才交给Engine；不足/存储失败/取消整体回滚，不发布部分结果，模糊commit仍保留耐久上界。普通单fragment API保留；未知结算及来源身份保持原语义。
- 新Store测试验证真实父hold留40+独立64来源：不足105不留下资金/速率/child，第二child故障全回滚，成功100保留64/36来源且reopen恢复全部上界，race三轮PASS2.900s。Agent原short-write/unknown/canceled/insufficient守恒测试race三轮PASS5.173s；Linux GOMAXPROCS4持续TCP/UDP+fragment原测试三轮PASS54.585s（/tmp/vortex-atomic-fragments-linux-continuous.log）。这些仍为开发验证，下一候选需完整same-SHA Docker和Actions。
