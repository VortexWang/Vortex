# Phase 1–7 整合验收：未放行

测试候选：`dd1beabd56db6f0abeb101b5266996f820bb7e81`。本报告是后续纯文档及证据提交，不宣称其自身重新完成运行验收。PR #2 保持未合并；主线仍为 `6e474c3067c33333d194d3b203bbc6229bebf01e`。

Phase 1–6 开发分支及 Phase 7 规划已被主线包含，六条源分支祖先关系再次核对；Phase 7 的 84 份计划与原规划分支逐文件一致，实施尚未开始。剩余是 PR #2 的回归收敛及最终合并。

| 验证 | 结果 | 原始运行 |
| --- | --- | --- |
| 本机 Docker 完整普通/race、故障、真实 Cloudflare CRUD | 通过；独立host哈希/语义复核通过 | 本机目录见下 |
| Phase 5 授权、完整恢复、证据清单、节点部署 | 通过；独立复核31进程场景/98快照/3431哈希 | Actions42858216510 |
| Phase 6 原生 arm64 | 完整通过，含真实Cloudflare；独立host复核通过 | Actions42858216231 |
| Phase 6 原生 amd64 | 失败：原60秒内未完成最终Hub资金审计 | Actions42858216231 |
| PR Phase 1–5、Phase 6 arm64 | 通过 | PR检查 |
| PR Phase 6 amd64 | 失败：FULL提交212.539ms阻塞后续原200ms授权 | Actions42858442481 |

精确候选的amd64并发续额完成了36.667秒业务流量、45.518秒Agent确认、56.608秒全部后端回执；最终资金审计触及60秒。原资金方程仍逐来源检查，没有删除断言或用回执数量代替最终精确审计。PR合并引用`dfd3ce81`与候选文件树相同。

端口竞态修复在完整Docker普通/race自动37规则续租中分别11.51/19.17秒通过。完整后端普通/race各502项、Node/Hub各1158项、Engine各301项；四真实进程race逐项通过，并发续额34.54秒。上述成功不覆盖amd64失败。

独立诊断`codex/diagnose-phase6-wal@03b0d9e2`只观察WAL与strace，不进入产品或充当验收。amd64捕获真实WAL fsync250.377ms，另一次失败准入fsync152.954ms发生于仅9帧且无checkpoint时，结合排队达到239–240ms，故不能凭猜测移动检查点来宣称修复。诊断普通通过/race失败均保留。初次诊断导出权限错误已明确记录，修正后原始产物完整。

在失败DB副本上用现有OpenReader比较，只省约0.5秒打开开销；完整逐行审计仍保留，尚不能证明60秒收敛，因此没有提交该推测性修改。仓库自托管runner数量为0；已请求可用专用Ubuntu24.04 amd64环境，等待配置位置。后续继续收敛200ms/60s，维持FULL、5秒恢复预算、未知负债及全部精确审计。

本机原始目录：`/private/tmp/vortex-final-dd1beabd-evidence.wiGKtGmt`。Actions原始zip、SHA256、运行ID、压缩日志及完整判定：`artifacts/phase6-integration/dd1beabd-acceptance/final.json`。用户原工作区、未跟踪quick计划、stash和源分支保留。
