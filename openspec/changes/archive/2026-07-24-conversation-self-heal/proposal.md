## Why

一次 wechat-bot 用户收到通用 "An error occurred during execution" 错误的根因定位揭示了核心事件脊柱的两个结构性缺陷：

1. **投影追加不幂等**：`projection.Append` 盲追加，同一框架事件（同 `event_key`）可被 onEvent 回调重复投影两次 → 出现重复的 `role=tool` 消息 → OpenAI 兼容 API 直接 400（"tool 消息必须紧跟对应的 tool_calls"）。
2. **发送前无校验**：组装给模型的完整 `messages[]`（在 BeforeModel 压缩后才可见）**没有任何 tool_call/tool 配对校验**。一旦投影有脏数据，400 不可避免。
3. **失败处理放大**：`event_loop` 对永久 4xx 仍盲重试 3 次（投影已污染，每次重建的 messages 同样畸形），纯浪费 + 可能连发多条通用错误给用户。

三层叠加 = 一条 tool 事件的偶发重复 → 无可修复地崩溃 + 用户收到一句无用的英文通用串。**需要分层自检自愈**（防重 → 校验修复 → 智能重试），让系统对历史脏数据鲁棒。

## What Changes

- **L1 幂等投影追加**（治本）：`projection.Append` 按 `EventKey` 幂等——同 key 事件只入一次（key==0 的未编号事件不去重）。精准消除 Class A 重复（同一框架事件双投影）。
- **L2 发送前自检 + 保守修复**（鲁棒兜底）：在 BeforeModel 链 SmartCompressor **之后**加一步校验：每条 `role=tool` 须有前序匹配 `tool_call_id`；无重复 `tool_call_id`；孤立/重复的 tool 消息 → 保守修复（删重复、孤立删除/成对裁去）后继续发送，仅作用于本次发送、不回写投影。
- **源头溯源诊断**（Phase 0.2）：在 `onEvent` 持久化边界记录 `evt.ID + event_key + role + type`，使下次重复复现可判别"框架重复投递"vs"键碰撞"两类根因。

## Capabilities

### New Capabilities
- `conversation-self-heal`: 对话历史幂等性保障（投影按 EventKey 幂等）+ 发送前 tool 配对校验/保守修复，使 RunFlow 对投影脏数据鲁棒。

### Modified Capabilities
<!-- 无 requirement 级变更到其它能力；事件循环行为不变。 -->

## Impact

- **代码**：`agent/projection.go`（幂等 Append + Replace 重建 seen）；新增 `agent/message_validate.go`（tool 配对校验+保守修复，BeforeModel 末端钩子）；`agent/context_manager.go`（注册校验回调）；`agent/session.go`（onEvent 边界溯源诊断日志）。
- **行为**：重复事件静默跳过（Append 打 warn）；畸形消息被保守修复后发送（打 warn）；正常路径零行为变化。
- **保持不变**：正常路径（无重复、无畸形）零开销（校验 O(n) 紧随已有 O(n) 压缩）。event_loop 重试逻辑不动。
- **非目标/后续**：~~L3 错误分类重试~~（实现期证伪：`RunFlow` swallow 模型错误、重试对其不触发，无放大 → 撤销）；Class B 内容重复去重（合法重发）；诚实透出真实错误（消费端 wechat-bot main.go 单独处理）；冥想 digest 对接。
