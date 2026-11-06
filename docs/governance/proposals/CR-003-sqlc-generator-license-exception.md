# CR-003：仅 sqlc 1.31.1 开发/CI 制品的 MPL-2.0 例外提案

状态：**待项目所有者决定；尚未生效。** 06-08 Task 2 许可门禁保持 blocked，当前许可白名单和版本锁未修改。

## 冲突与证据

项目已经批准 sqlc v1.31.1 的精确官方制品，但此前将项目根许可证 MIT 当作整个生成器的许可结论。实际 `go version -m` 显示官方二进制链接 `github.com/go-sql-driver/mysql v1.9.3`，module sum 为 `h1:U/N249h2WzJ3Ukj8SowVFjdtZKfu9vlLZxjPXV1aweo=`。驱动的许可证是 MPL-2.0，不在当前白名单中。

- 政策：[license-policy.yaml](../license-policy.yaml)，第 5 行允许列表没有 MPL-2.0；第 9 行范围包含 generators 与直接/间接依赖；第 14 行要求审查链接依赖。第 6 行未明确列出 MPL 不等于批准 MPL。
- 精确驱动 LICENSE：https://github.com/go-sql-driver/mysql/blob/v1.9.3/LICENSE ，SHA-256 `fab3dd6bdab226f1c08630b1dd917e11fcb4ec5e1e020e2c16f83a0a13863e85`。
- sqlc `internal/cmd/vet.go` 包含 `_ "github.com/go-sql-driver/mysql"`，该文件没有可用于禁用该导入的 build tag：https://github.com/sqlc-dev/sqlc/blob/v1.31.1/internal/cmd/vet.go 。这不是只出现在 go.mod 中的未链接条目。
- 已验证 Darwin arm64 二进制 SHA-256：`7e63a8b2166f1103a2ad1d2e5d05d8fe3f13ca65ecf2774a2570b830d32055fd`。完整 build metadata 与门禁结果位于 `artifacts/phase6/06-08/license/`。

## 最小例外范围（建议）

仅允许下列**官方未修改 sqlc 生成器压缩包**中 `github.com/go-sql-driver/mysql v1.9.3` 的 MPL-2.0 代码，用于 VORTEX 开发和 CI 的 SQL → Go 生成。制品来源前缀为 `https://github.com/sqlc-dev/sqlc/releases/download/v1.31.1/`：

| 文件 | SHA-256 |
| --- | --- |
| `sqlc_1.31.1_darwin_arm64.tar.gz` | `21602158c99eb1f2bae197a66abfb1941d1e9e50b23125bb193349c6b1acc71e` |
| `sqlc_1.31.1_linux_amd64.tar.gz` | `497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354` |
| `sqlc_1.31.1_linux_arm64.tar.gz` | `b7cae247740d0c51a1e657479e5b2d21e6fef428f596682a01bc55bf4ab8a23d` |

这些摘要已经在版本锁和安装脚本中锁定。当前证据对 Darwin arm64 原件完成了 archive/binary/build metadata 核验；Linux 两项是已锁定的待恢复执行环境中继续核验的制品。例外不能扩展到其他版本、修改重编译制品、其他 MPL 包或 VORTEX 产品链接依赖。

当前用法：`tools/phase6/install_sqlc.sh` 在开发临时目录或 Compose 测试容器 `/tmp/phase6-tools` 下载并执行生成器。VORTEX 后端 Go 模块不导入 sqlc 或此 MySQL 驱动；生成的查询源代码使用 pgx/v5。没有把 sqlc 二进制加入现有 VORTEX 产品运行制品。开发/CI 基础镜像和将来可分发的工具镜像仍须按实际分发范围审查，不能借此例外豁免整幅镜像。若将来随产品或工具镜像分发 sqlc，则先完成下列再分发义务并另行记录分发清单。

## 义务与实施边界

以[官方 MPL 2.0 正文](https://www.mozilla.org/MPL/2.0/)及[Mozilla FAQ Q5–Q12](https://www.mozilla.org/en-US/MPL/2.0/FAQ/)为依据：

1. 单纯内部使用不触发向外分发源码的义务。生成查询代码不会把生成器二进制自动链接到 VORTEX；仍独立检查生成文件内容和最终模块依赖。
2. 若对外分发含覆盖代码的可执行形式，按 3.2(a) 告知接收者如何取得覆盖代码的源码，并按 3.1 保持该源码以 MPL 提供；不能限制接收者在该许可证下的权利。保留 3.4 要求的版权及许可通知。
3. 若修改并分发 MPL 覆盖文件，应提供对应覆盖文件及修改的源码；本提案不授权修改生成器。
4. MPL 3.3 允许 Larger Work 使用其他条款，但不消除覆盖文件本身的义务。这不是将 VORTEX 产品依赖白名单整体放宽为 MPL。
5. 保留 sqlc MIT 和驱动 MPL 的 LICENSE/NOTICE、精确源码地址、制品校验值及用途清单。正式分发前验证上游包的 3.1/3.2 通知和源码获取说明充分，不能仅凭根目录 MIT 标签放行。

若所有者批准，后续实施只加入明确的工具例外记录与精确制品匹配逻辑，继续拒绝非白名单产品依赖，并完成生成器余下编译依赖的闭包复核后重新执行门禁。当前 gate 未接受任何例外。

## 可选决定

| 选项 | 结果与代价 |
| --- | --- |
| A：批准上述最小开发/CI 生成器例外（建议） | 保持已批准的 sqlc 版本和制品；增加明确许可义务与分发约束；后续完成闭包复核并重跑 gate。 |
| B：不批准，重新规划生成器制品 | 保持许可白名单不变；需要单独评审新的 PostgreSQL 专用构建/替代方案、依赖许可、生成等价性和新 checksum。当前没有无修改官方制品的配置可移除已链接驱动。 |
| C：暂缓决定 | 保存已提交的 Task 1 与独立验证，06-08 Task 2 及后续依赖波次保持 blocked。 |

待决定：A / B / C。批准 A 只批准上面的具体例外，并不代表 Phase 6 整体验收通过。
