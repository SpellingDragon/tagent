# 01 send-tool-face：send 工具面唯一化

父变更：send-cross-task-session（依赖：无——本域产参数面常数，是 02/03 的前置）。

## Why

send 的 Declaration 与 CHANGELOG 口径分裂（出网送达 vs 跨代理通信）；R2 实证"参数只有 content、目标取调用地址并进外发通道"。用户裁定 send 唯一职责=跨 task、会话通信。

## What Changes

- `hostSendTool` 声明重写：职责文案改"跨 task、会话通信"，删除"Deliver one body to the user address of this call"。
- 参数面 `target` 对象二选一（`{task}` 或 `{agent, session}`）+ `content`（8KiB 上界保留）；未知字段/双目标/空目标具名拒绝。
- 旧出网语义移除：send 不再走 `hostDeliverFunc` 出网（出网唯一路径=回合正文自动投递，不变）。
- 装配规则：声明 send 不再要求 sender 存在（`ErrHostSenderMissing` 的 send 分支删除，用户会址回合分支保留）。

## 边界与依赖

- **依赖**：无（本域产接口常数：`target` 字段名/回执文本格式/`hostSendToolID`——02/03 引用为常数，开发可并行，集成需本域合并）。
- **被依赖方**：02（`{task}` 形态）、03（`{agent,session}` 形态）、04（prompt 引用）。
- **接口面**：`hostSendTool` 参数 schema、`[send_denied]/[send_held]` 回执格式、`hostToolIDs` 装配面。
- **禁止事项**：不动 `hostDeliverFunc`/`deliverObligation` 出网链；不动 `MessageSender` 红线；不实现重入与跨会话投递本身（02/03 域）。
