# Phase 7 — Prohibition Recall and Precision

无SPEC，按逐要求recall→precision处理。序列化使用运行时bin/lib/probe-core.cjs的projectProhibitions实际输出，非手写第二序列化器。4条bespoke保留项为status:unresolved、verification:null的原始项；canonical投影省略null verification，不含check_*。因此下游必须flagged-unverified，不能把本文件或未来测试路径当已wired机械检查。后续真有非空负测及fail-first证明后另行接线，不在规划伪造descriptor。

## Recall ledger

| Requirement | Raw intent candidates surfaced | Precision disposition / canonical breadcrumb |
| --- | --- | --- |
| FE-001 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-002 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-003 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-004 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | KEEP 不得将受理、断流或超时呈现为节点已执行、业务可用或已经停止。；常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。 |
| FE-005 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-006 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-007 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-008 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-009 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-010 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-011 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| FE-012 | 缺失/unknown伪装成功；业务影响被隐去；无权用户误获入口权力；秘密随截图泄露；跨身份草稿/通知混用；英文/错误无建议；空数据崩溃；乱码长度；重试重复；权限竞态 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-001 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-002 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-003 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-004 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-005 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-011 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-012 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-013 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-006 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-007 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-008 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-009 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SEC-010 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | KEEP 不得把未知负债计为已消费、跨来源抵销，或用备份旧余额恢复新授权。；常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。 |
| SUP-PAGES-001 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | 常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。本要求无新增独立bespoke项；已由同类资金/权威保留项覆盖的语义不重复mint。 |
| SUP-RECOVERY-001 | 旧余额/未知证据被释放；恢复创造额外权力；跨租户影响误扩；来源身份被合并；密钥托管名存实亡；日志泄密；注入/路径穿越；空值；竞态；错误重试 | KEEP 恢复网页、连接恢复或旧PG状态不得制造新generation、可信批准、FULL成功或延长原自治截止。；不得以Hub消费账本重建兑换/套餐事实，或因一个租户核清而放开其他租户。；常规空值/编码/并发/重试归edge或review，DROP；OWASP认证/注入/SSRF/secret hygiene/保留合规等canon→$gsd-secure-phase与相应lint/security suite，不mint重复禁止项。 |

## Kept bespoke items (flagged-unverified)

| Plan | Requirement | Statement | Lifecycle | Verification | Descriptor |
| --- | --- | --- | --- | --- | --- |
| 07-31/07-32/07-33/07-34/07-35 | FE-004 | 不得将受理、断流或超时呈现为节点已执行、业务可用或已经停止。 | unresolved | null | absent |
| 07-51/07-52/07-53 | SEC-010 | 不得把未知负债计为已消费、跨来源抵销，或用备份旧余额恢复新授权。 | unresolved | null | absent |
| 07-78 | SUP-RECOVERY-001 | 恢复网页、连接恢复或旧PG状态不得制造新generation、可信批准、FULL成功或延长原自治截止。 | unresolved | null | absent |
| 07-81 | SUP-RECOVERY-001 | 不得以Hub消费账本重建兑换/套餐事实，或因一个租户核清而放开其他租户。 | unresolved | null | absent |

Coverage: applicable4/resolved0/unresolved4，byVerification test0/judgment0。descriptor-less不是测试通过；规范/计划已有具体行为负例也不能替代canonical fail-first接线。安全canon候选只留上表breadcrumbs，不重复mint。

