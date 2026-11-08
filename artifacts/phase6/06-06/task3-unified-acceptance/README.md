# 06-06 统一候选验收

精确源提交：`cf92b7e24b6c557a5d14a18b0549a67eac349eb4`。从干净克隆运行扩展后的06-06 Docker入口，输出在源树外；使用独立Compose project和全新PG，保留其他开发数据。源码前后干净，原始producer退出0。

| 检查 | 结果 |
| --- | --- |
| 完整本机backend race | 通过，service334.663s；开发checkout辅助证据 |
| 干净Docker backend normal/race | 通过，service211.753/359.668s |
| Agent/Store/Hub/contracts/Engine normal/race | 通过；Store68.931/164.358s，包含隔离full-disk试验 |
| ledger fault | 通过，2.820s |
| share/recovery fault | 通过，139.984s；含实际60秒lease、SIGKILL和FULL丢回执/重放 |
| Linux amd64/arm64 Node/Backend/Hub构建 | 六项通过；Engine包双架构编译亦通过 |
| 两次独立sqlc/protobuf生成、依赖/许可、旧wire、OpenAPI | 通过，见本目录独立原始日志及Docker内生成比较 |
| 源码/SHA/关键用例/构建元数据验证 | 通过，clean-docker-validation.json |
| 错SHA、脏源码、缺fault、skip、缺关键runtime pass | 五种均拒绝，evidence-negative-checks.json |

Linux实际执行架构arm64，镜像与宿主元数据见原始Docker目录。不代表nativeamd64、Ubuntu24.04资格、专用性能、完整模型或物理断电。最终同SHA Actions配对保持未执行；普通Linux测试不是外部Cloudflare真实CRUD证明。全成员转发/滚动升级/重启范围见LIMITS.md。

更早一次开发Linux renewal-loss检查出现额外重连，原因未确定，失败保留于task3-older-path-revocation/；后续诊断、完整affected和本次干净normal/race/fault均保留严格断言并通过。不得将原失败改写为已定位。

CR-008仍待owner批准，规范摘要`4e29497f82808b9a8693030ef400dae3a2bc42cc27cf8a39f24bdcf8605074bd`。本目录不是approval，也不标记06-06完成。Phase6仍8/11。
