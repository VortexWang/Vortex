# CR-004：sqlc 内嵌来源与剩余许可补充审查

状态：**待决定；没有新增批准。** CR-003 A 已按所有者原话“按推荐”实施，仅覆盖 mysql/MPL。06-08 Task 2 保持 blocked。

## 已完成的实际制品审查

三个官方 sqlc 1.31.1 原件的 archive/binary 摘要、URL 和 42 个链接 Go 模块的精确版本/sum/许可文件摘要见 `tools/phase6/sqlc-dependencies.json`。完整通知见 `docs/governance/notices/sqlc-1.31.1.txt`。后端 24 个编译模块不能代替生成器闭包。

三种 binary 都逐字节包含以下完整资产，不是仅在 go.mod 中列出：

| 载荷 | 长度 | SHA-256 | Darwin arm64 / Linux amd64 / Linux arm64 字节起点 |
| --- | ---: | --- | --- |
| SQLite Wasm | 1408228 | `4bd039600eae69059a672ef1c063f4d8ecc94e6367cf742ea903ca412035e0f8` | 16922304 / 23524256 / 21481120 |
| libpg_query Wasm | 2211002 | `a49afdfd4f711aca1510c17b062036f45dcf07ad5363d16a586f9a9980c1dec5` | 49192416 / 51515200 / 49176160 |

上游 module sum 认证供应的资产和源码；本次没有宣称独立复现上游 Wasm 构建。

## 两个已知的精确许可差异

1. **MIT-0 分配器**：`github.com/ncruces/go-sqlite3 v0.32.0`，sum `h1:hNBUXp88LrfQCsuyXLqWTbTUG35sUuktDsqhhgHvU20=`。`sqlite3/libc/malloc.c` SHA-256 为 `be7be56b40b4b31417e74e77687f9b96127ea6cae534cf236aee40a4c739508d`。[精确源码](https://github.com/ncruces/go-sqlite3/blob/v0.32.0/sqlite3/libc/malloc.c) 明示 Doug Lea 2023 MIT-0，取代旧 CC0；[构建脚本](https://github.com/ncruces/go-sqlite3/blob/v0.32.0/embed/build.sh) 以 `-nostdlib` 编译 main.c，经过 malloc_sbrk.c 第 38 行 include malloc.c，再由 Go embed 链接匹配的 SQLite Wasm。不是仅在源码仓库存在。[SPDX 正文](https://spdx.org/licenses/MIT-0.html) 与 [OSI 正文](https://opensource.org/license/mit-0/) 免除 MIT 的通知保留条件，不要求开放 VORTEX 源码；项目仍保留其来源、版权与全文。当前允许列表没有 MIT-0，CR-003 不覆盖它。

2. **Bison 输出例外**：`github.com/wasilibs/go-pgquery v0.0.0-20250409022910-10ac41983c07`，sum `h1:mJdDDPblDfPe7z7go8Dvv1AJQDI3eQ/5xith3q2mFlo=`。其[精确 SQL 解析器](https://github.com/wasilibs/go-pgquery/blob/10ac41983c07/internal/cparser/src_backend_parser_gram.c) 与 PL/pgSQL parser 明示 `GPL-2.0-or-later WITH Bison-exception-2.2`；文件 SHA-256 分别为 `347dc92a939d312ac6e727f4018583314b181765f532427d2f2d365d552ace5a`、`9ef438ff195798f682404f5d0446bfee0de0ea20407cf95c26a91e3baa1604fb`。构建脚本编译这些 C 文件到匹配的 libpg_query Wasm。[GNU 说明](https://www.gnu.org/software/bison/manual/html_node/Conditions.html) 与 [SPDX 例外正文](https://spdx.org/licenses/Bison-exception-2.2.html) 允许含骨架的 larger work 自选条款，但不包括把骨架用作解析器生成器骨架。sqlc 使用现成 SQL parser 生成 Go 查询包装，不输出 Bison 骨架。建议明确认可**这些精确制品中的该输出例外适用性**；不批准普通 GPL，不允许删除例外、修改骨架或改变用途。当前门禁在明确决定前拒绝该未列出的表达式。

## sysroot 来源恢复及尚未证明的部分

已恢复 SDK OCI index、arm64 manifest/config/labels/history：镜像 revision `fc94d603e92e2597fc605261127e9aabcf0a30b1`，arm64 digest `sha256:91bbb2135e2a74b67ee92cebb9e899eb32e1e1ed8431b72b3b079bf62a87f1ae`。直接下载并校验 COPY sysroot 层 `sha256:e651b9e28ec28d7fd702f4fffcec24a599c5a3ec16e869cd71f3992981fb86be`，恢复 1128 个普通文件及静态库，层内没有 license、git revision 或完整来源清单。原始证据与逐文件摘要位于 `artifacts/phase6/06-08/eabc8f6/license/`。

进一步从已链接 Wasm 的 DWARF 恢复到 `wasisdk://v12.7g77ba98a998cb`，对应 SDK commit `77ba98a998cb9f2a63ab3a5f94bbabd069f65ff0`；其 gitlink 确认 LLVM `d28af7c654d8db0b68c175db5ce212d74fb5e9bc`。历史 libc 候选源码 `d0362cb23d5a6bdba3ddb2e04748ebb55b48cd00` 内预置 builtins 归档的 **146/146 对象**与恢复的 libc.a 内对象完全相同；Wasm DWARF 明确列出实际链接的 13 个 compiler-rt 源文件。因此 compiler-rt 已有精确来源，不再把它当完全未知。

[该 LLVM 版本 runtime 许可证](https://github.com/llvm/llvm-project/blob/d28af7c654d8db0b68c175db5ce212d74fb5e9bc/compiler-rt/LICENSE.TXT) 是 Apache-2.0 WITH LLVM-exception。这里保留完整表达式和例外；按已经允许的 Apache-2.0 基础义务保留通知，不依赖额外例外豁免。sysroot 中存在 libc++/clang 或其他归档，不等于它们全部链接；没有把整幅编译器镜像包清单当作运行时代码清单。

原 run `6268266259` 的匿名和已认证 API 都 404，历史 commit check-runs 为零，build32.sh 也拉取未固定的 WASIX ABI main。因此不能猜原 run 的 checkout SHA。现已用对象级源码恢复替代该缺失记录：在精确 SDK 镜像中离线重编，**1086 个非 builtins 归档成员的全部 Wasm section 均相同，仅 producers 的编译器发行版名称不同**；CODE、DATA、类型、导入、符号、重定位和 target_features 全部保留比较。另 146 个 builtins 成员逐字节相同，1232 个成员无未恢复代码。比较器和拒绝代码/节漂移、保留重名 ar 成员的测试随工具提交。

随后按 module 的原构建配方重新编译并链接 libpg_query，保留 link map/trace：只选入 libc.a 的 **107 个 libc 对象和 13 个 compiler-rt 对象**，全部在已匹配集合中。没有选入 libc++、另一个 compiler-rt 归档、独立 emulation 归档或 regex 实现。实际唯一 math 源是 musl 的 frexpl，按 musl MIT 许可；cloudlibc 为 BSD-2-Clause，WASI 修改有 MIT 选项，旧 dlmalloc 明确 public-domain/CC0 dedication，使用现有的显式 Public-Domain 分类并保留原文。它不是 ncruces 的新 MIT-0 allocator。107 个源码文件摘要和 13 个确切 LLVM 源文件已保存，来源恢复范围内没有待批准的未知许可。

来源证明的限制：这是供应的精确 Wasm 资产、上游构建源码、恢复的 SDK、归档对象比对和重链接选择记录；没有宣称优化后的原 Wasm 已独立逐字节复现。旧 run metadata 仍缺失，不能伪造；它不再是未知链接代码许可的唯一证据路径。

## 未作为实际链接冲突的通知

- structtag LICENSE 中的 gopher CC-BY-3.0 图片通知保留；sqlc 没有链接该图片。
- Spencer-99 仅在未使用的 regex 声明头；没有把头文件存在冒充 regex 实现链接。若以后分发完整源包，继续保留其原始通知。
- protobuf-c、xxhash、strlcpy、snprintf、PostgreSQL 等 BSD/ISC/MIT 通知分别保留，不能仅保留 Wasm wrapper MIT。

## 一次性的具体决定

| 选项 | 授权与后续 |
| --- | --- |
| A（建议） | 只对 CR-003 同三个 archive/binary checksum 锁定、官方未修改的 sqlc 开发/CI 制品，批准上述精确 ncruces 模块/allocator 的 MIT-0，并认可上述精确 wasilibs 模块/parser 的 Bison 输出特殊例外适用性。保留通知及源码链接，不改全局白名单，不允许普通 GPL、删除例外、修改重编译制品或产品运行时链接。实施精确匹配后重跑完整门禁，仍须验证通过才能完成 Task 2。 |
| B | 保留 CR-003 已批准部分，其他项不批准；重新规划具有完整来源和许可清单的生成器制品，单独审查生成等价性、版本和 checksum，不自动替换。 |
| C | 暂缓，保留已提交实现和证据，Task 2 及依赖波次保持 blocked。 |

任何选择都不批准未知许可。如果以后对外分发生成器或工具镜像，另行建立分发清单、许可证/NOTICE 与适用源码提供义务；当前内部开发/CI 授权不覆盖该分发。本文保持 pending，不能把 CR-003 的“按推荐”扩展为对本文的批准。
