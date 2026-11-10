# CR-011：VLink 详细规范与精确批准记录补齐

状态：待用户精确批准。此提案没有更新规范事实路径、冻结记录或历史验收。

## 问题与来源

Phase 1 CI 的 approved review hash is missing or stale 是真实治理缺口。原批准 SHA-256 dbf1607d306720a73053848fff1ca92e1d66716957a0410f83c7c1552a112b52 绑定 7c4600af 的35行 Draft；后续7d97b4ae写入详细帧与身份规范，6310aeff补充OPEN总长42..310字节，但未同步精确批准。两条合并来源均已有该缺口。现行97行规范 SHA-256 bf118a61789d37854f7c18dc5676d62bac4b845040944812418419cdacb17a9e。

因此这不是仅更新一个错误摘要，也不是只审批两行 HELLO 文本；需要审阅从旧 Draft 到现行详细规范的全部差异。完整差异见同名 .diff，完整候选见 .encoding.md，机器摘要见 .json。

## 请求批准的规范范围

采用 contract catalog 1.1.0，正式记录现已实现的完整 VLink v1 规范：TLS 1.3和UUID身份/吊销；三消息HELLO与nonce/boot/generation/TLS exporter绑定、签名算法及长度；8字节header、各帧代码/stream范围/载荷布局和OPEN字段边界；TCP早期DATA有界授权、UDP OPEN_OK门槛、半关闭/重置与流/链路失败范围；字节窗口、连接复用和有限度量标签/事件约束。候选只调整现行规范的版本与历史记录文字，具体运行wire内容保持现行实现。

版本1.1.0用于明确化先前Draft的未分配细节；已有节点间协议仍是0x0100，不改变HELLO编码、帧字段/代码、算法、授权有效性或协商。不宣称兼容任何自设Draft编码的外部实现；此提案也不授权新wire行为或放宽安全边界。

## 批准后准确执行

1. 把同名 .encoding.md 的精确候选字节写入 contracts/vlink/v1/encoding.md，更新encoding.sha256和catalog版本1.1.0。
2. governance记录新精确摘要与本提案摘要、用户批准时间，同时保留旧批准原记录，不改写为曾批准新字节。
3. 在CHANGELOG加入compatible版本记录，对现有所有VLink需求引用加本次review metadata，不删除任何映射。
4. 重跑完整冻结/需求映射/SemVer门禁、VLink测试及本机/Actions，失败保持失败。本批准不会把历史有限测试变成性能或全系统验收。

CR-010只批准control迁移，不覆盖此提案。当前审查修复F-01/F-06不修改wire规范；批准CR-011仅关闭以上既存治理缺口。
