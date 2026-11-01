# 04 技术要点

## 要点

1. **迁移原则**（父变更 D 系列 + 测试迁移六原则）：场景保真——"模型 send 被 hold"改为"send 新形态被 hold"；不删场景、不改断言语义。
2. **prompt 改写口径**：`resume_task(task_id, input)` → `send` 携 `target: {task: …}`；"一个 change ≙ 一个 task id"协议保留，只换运输工具名。
3. **wiki 契约引用**：`wechat-host-send-tool` 主 spec 归档后同步（04 在 F4 前完成，wiki 引用 spec 条款名逐字符核对）。
4. **重写=保全+重组+增量**：文档改写先提旧版细节清单（日志关键字/常量/特例），改后逐项核对，未迁移项显式说明。

## 文件清单

- 改：`resources/prompts/*.md`、`examples/wechat-bot/resources/prompts/*.md`、`docs/wiki/**`、`CHANGELOG.md`、`examples/wechat-bot/*_test.go`、`tests/integration/host/*`。

## 风险与回退

- 风险：prompt 改写影响 smoke 脚本模型对工具名的断言（`smokePairedResult` 等按 callID 配对，不受工具名影响；`smokeApprovalDocument` 按 id 补丁需同步）。
- 回退：纯文档/测试域，可独立回滚。
