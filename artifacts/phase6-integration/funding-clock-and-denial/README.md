# 资金块可信时间与全拒绝事务回归

本目录记录开发回归，不能替代同一干净 SHA 的完整 Docker、双架构 Actions、Phase5 恢复部署验收。

普通首次/已有资金块和批量准入原来沿用入队前的可信时间；现在与 compact 路径一致，在取得 SQLite writer 事务后重新采样已认证的保守时钟。Agent first/existing 两个确定性负例在旧路径返回成功，修复后拒绝。Store 的 first/existing/batch × active/expired/untrusted/revoked/canceled 共15例验证实际排队、事务内时间、每次 rate 扣减以及资金守恒。未缓存跨事务权限或修改期限。

所有成员都被确定性拒绝时，成员 SAVEPOINT 虽已回滚，外层 COMMIT 仍能写出脏页。独立只读 SQLite 连接的 data_version 在旧实现从2变3；修复后完全回滚且不变。各成员原错误不变，任何成功成员及混合结果组仍要求原 FULL COMMIT；取消、模糊失败和回滚错误继续拒绝。

| 验证 | 结果与边界 |
| --- | --- |
| 确定性旧路径 RED | Agent 两例与全拒绝 data_version 负例失败，保留原日志 |
| Clock-only 完整本机 Linux arm64 race | Store330.742s、Hub32.542s、Agent260.818s通过；该次 Agent 未提供真实进程二进制，四例明确跳过 |
| 组合修改完整 Store/Hub race | 293.960s / 21.323s通过，退出0 |
| 组合相关取消/混合结果/时间/拒绝 guard race | 13.119s通过 |
| 专用 Ubuntu24.04 amd64 clock-only overlay | 新时间15例和连续流量通过；四真实进程失败，Agent60.454s/退出1 |
| 专用 Ubuntu24.04 amd64 组合 overlay | 新拒绝与时间回归通过；连续流量19.44s及四真实进程8.04/5.91/8.92/24.71s全部失败，Agent70.000s/退出1 |

专用组合失败不能改写为通过。并发续额一次前序 checkpoint FULL107.008503ms+work27.710996ms，后继准入持锁48.949943ms，叠加后超过200ms；并发TCP有成功FULL51.510973ms+work32.829517ms，含排队总计224.5735ms，节点按原边界拒绝。全拒绝外层回滚减少已证明的无效提交，但没有解决这台现有磁盘上的整体延迟。

用户确认只有当前专用主机及磁盘。没有修改200ms/60s/5s、FULL/WAL、真实流量次数、最终逐来源账本审计或unknown冻结；不重复请求另一台主机，不以相同代码重跑碰运气。PR2保持未合并，Phase7只包含规划。

`final-source-sha256.json`绑定八个修改源码；专用overlay基于dd1beabd并叠加已提交夹具排序、checkpoint行数修复及本轮修改，属于开发覆盖验证，未冒充干净提交验收。全量archive含测试临时密钥，未提交；`archives.json`记录本机原始路径与完整哈希。仓库仅留存压缩日志，诊断脚本及哈希。

组合完整Agent race本次退出1/256.912s：连续资金块TCP/UDP通过14.56s，但普通自适应TCP在后续续额等待中触及原context。该普通路径不调用本轮资金块函数；原因尚未确定，不能凭其后隔离通过判定已修复。首次调用漏建VORTEX_PROCESS_EVIDENCE_DIR，四真实进程在启动前失败，完整运行不计通过；补建父目录后单独执行原四场景，结果另记。原始完整失败JSONL保留。

补正父目录后，原普通自适应续额与四真实进程Linux arm64 race均通过（整个命令61.957s，分别11.49/5.44/5.27/6.14/32.60s）；单个真实进程仍各受原60秒预算。该隔离通过不覆盖前述完整Agent失败，也不证明其根因已消失。生产源码八个hash从完整Store/Hub到本次结束均未变化。未启动新的完整正式验收，因为已有专用原200ms门禁失败及完整Agent待查项。
