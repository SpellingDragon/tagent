# 06 notice-inbox-contract：通知持久接纳与来源分账（R1/R4）

父变更：send-cross-task-session（依赖：无——与 01 并发）。

## Why

R1（✅ 实证）：`InjectInternal` 只入内存，随后立即把通知文件改 `.notified`/`.consumed`——入箱与改名之间崩溃=通知永失；`Drain` 在 `Invoke` 提交前清空——提交失败即丢。当前标记证明的是"入箱/取走"，无法证明持久受理或最终消费。R4（✅ 实证）：`source` 只写日志，入箱消息与用户输入合并后统一 `TriggerSource: "user"`，结构化来源丢失。

## What Changes

- **持久接纳记录**：启动通知先入稳定身份的持久接纳记录（身份=来源+内容摘要+接纳序号，重投同身份幂等），可靠交接（输入提交成功）后才完成 `.notified`/`.consumed` 标记；改名失败保持待接纳（不再"下次重报"语义漂移）。
- **消费确认**：`Drain` 返回副本但消费确认（Invoke 提交成功）前不清空；失败保留在册可重投。
- **来源分账**（R4）：逐消息原始来源（`reincarnation`/`system_alert`/`receipt`…）随消息进持久事实 Metadata（`event-metadata-contract` 键词表扩展），与回合级 `trigger_source` 分账；不解析正文可查。

## 边界与依赖

- **依赖**：无（`event.MetaKey*` 键词表由本域单写——共享面，编排者核准）。
- **被依赖方**：07（运维通报的目标/唤醒接在本域接纳面之上）。
- **接口面**：`internalInbox`（InjectInternal/Drain 语义扩展）、通知文件标记时序、`MetaKey*` 新键。
- **禁止事项**：不把启动通知挂接 task 受理面（启动通知非 task，D9）；不动 `wechat_notice.go` 的目标/唤醒语义（07 域）；不改回合 `trigger_source` 盖章语义。
