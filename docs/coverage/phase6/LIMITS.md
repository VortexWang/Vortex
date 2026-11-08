# 06-06：份额、运行租约、资金授权与恢复证据

候选 CR-008。Phase6仍8/11；06-06的完成需要精确 owner approval。PG 是事实来源；share、runtime lease、credit 和账本 final 分别证明自己的边界。HTTP状态/管理适配器属于06-07，最终同SHA Docker/Actions配对属于06-11。

| 需求 | 生产路径 | 主要可执行证据 |
| --- | --- | --- |
| LIM-001 | 独立 runtime/share lease，60秒续签，TTL最多300秒；10秒 FULL heartbeat；credit不延长 | TestRuntimeLeaseActualMTLS60SecondsAgentInstallAndLostBackendReceipts；Store runtime renewal/lease tests |
| LIM-002 | PG tenant/node/kind线性chain与逐节点rate aggregate | TestShareConcurrentLinearChainCapacityKindsAndAuditRollback；TestShareHandoffRateCapAcrossSessionsDirectionsAndNoCASRefill |
| LIM-003 | connection独立kind与bootstrap总量；全自托管豁免 | TestShareSignerAndFenceAuditRollbackAndSelfHostedExemption；TestShareHandoffConnectionReductionWaitsForRealRuntimeDrain |
| LIM-004 | 预留max、完整旧续租关闭、Node FULL CAS、匹配回执后释放差额 | TestShareRenewalClosureCompletePagesAuditRollbackAndCASOnlyRelease；TestShareHandoffRealMTLSAgentAndPostgresWorker |
| LIM-005 | 用户总量、rule约束和最多一秒burst交集 | TestShareBurstSignerTimeAndRenewAuditRollback；TestShareHandoffAdditionalPlatformRuleCannotBypassAggregate |
| LIM-006 | rate切换回执、connection真实排空；unknown保持占用 | TestShareHandoffConnectionReductionWaitsForRealRuntimeDrain；TestShareHandoffLostReceiptRestartRetainsAndConfirmsExactDifference |
| LIM-007 | nonce/boot/epoch/generation独立相关；旧credit按来源final | TestShareLeaseDeadlineNonceBootFloorAndRenewalClosure；TestEffectiveRuntimeFundingAfterInstalledOldCreditFinalKeepsOriginalShare |
| LIM-008 | Prepare不等于激活；直接Commit、丢回执、持久floor/实际Engine查询 | TestShareHandoffFaultSIGKILLAfterDurableCASAndExpiredWorkerRecovery；TestRuntimeRecoveryPendingEngineCommitAutomaticallyRenewsAfterSR |
| LIM-009 | 60秒PG candidate window、50%保底、稳定整数余数、真实连接队列与引用复用 | TestDistributionPropertiesConservationFloorAndPermutation；TestAllocationWindowPreparedCandidatesReuseAndDemandAuditRollback；TestAllocationWindowConnectionShortagePersistsRealQueue |
| LIM-010 | 显式Node boot关闭/停止或安全截止；Hub恢复独立manifest/fence/root/admission | TestNodeBootRecoveryProofFullReceiptPreservesFloorsAndDebtUntilRealCAS；TestNodeBootRecoveryActualMTLSCapabilityLostFULLReceiptReplay；TestHubRecoveryAdmissionActualMTLSExplicitProofFULLLossReconnectAndNewIssue |

生产初始化要求显式 signer epoch/platform授权、独立可信时间和角色mTLS。未知旧parent、漏child、缺完整seal/final、未确认prefix、旧boot或当前资格不匹配均不能通过重试获得新资金。恢复保留原source/limits/deadline，只能在每项独立FULL证明完成后推进。旧path纯撤销不能推进当前floor或释放未知债务。

统一开发证据目录：artifacts/phase6/06-06/task3-unified-acceptance/。执行中的检查在原始日志完成前不计通过。精确提交的Docker入口：`PHASE6_THROUGH_PLAN=06-06 sh tools/phase6/docker_check.sh`；扩展包含完整backend normal/race、Agent/Hub/contracts/Engine normal/race、ledger/share fault suites、两次独立sqlc生成和Linux双架构构建。使用新Compose project和端口得到空PG，避免已应用candidate8旧schema的误用。

此入口产出candidate运行证据，不能授予contract freeze；tools/phase6/check.sh的双宿主总验收仍待06-11统一扩展。tools/phase6/limits_contract_check.sh独立校验已批准CR-007原快照、当前精确候选、旧wire兼容及两次protobuf生成。

边界：S/R pending Commit故障案例仅选定成员运行真实Engine，另一固定成员用已准备fixture及真实容量预留；不声称全成员转发、滚动升级或重启验收。SIGKILL、FULL回滚、磁盘故障及有限property算例不代表物理断电/完整模型证明。交叉编译不代表native amd64执行；专用性能/物理恢复/24h及外部Cloudflare凭据验证保持其原阶段状态。
