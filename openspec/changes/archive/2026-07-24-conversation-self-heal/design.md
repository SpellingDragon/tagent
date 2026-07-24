## Context

事件脊柱：`EventBus.Publish` → `runEventLoop.Pull` → `BuildInvocation`(合并 external_input 为一条 user 消息) → `RunFlow`。历史(投影)由多条路径追加：onEvent 回调(框架事件，`event_key` 来自 StateDelta，稳定)、`persistBusEvent`(总线事件，每次新 Snowflake)、Compactor `Replace`。发给模型的完整 `messages[]` 由框架 BeforeModel 链中的 `SmartCompressor` 从投影组装。

已确认：`projection.Append` 无按 key 幂等；发送前无 tool_call/tool 配对校验；`event_loop` 对任何 RunFlow 错误盲重试 3 次。一条框架 tool 事件被 onEvent 投影两次(同 `event_key`)即产生重复 `role=tool` → API 400。

## Goals / Non-Goals

**Goals:**
- 同一事件(同 EventKey)在投影中至多一次(幂等)。
- 发给模型的 `messages[]` 始终 tool 配对合法(校验+保守修复)。
- 重复注入的源头可溯源(诊断日志判别两类根因)。

**Non-Goals:**
- Class B 内容重复(如两条"继续")的去重——可能是合法重发。
- 修改 `SmartCompressor` 压缩算法；重写事件持久化路径。
- 冥想 digest 对接(L5，跨能力，后续)。

## Decisions

- **D1 幂等在投影层，按 EventKey**：`Append` 记录已见 key 集合；`EventKey>0` 且已存在则跳过并打 warn(含来源线索)。`EventKey==0`(未编号)不去重(否则会误collapse)。`Replace`(压缩)重建已见集合。这是持久层的单一治理点，对所有追加路径生效。
- **D2 校验/修复挂 BeforeModel 链末端**：只有组装后的 `args.Request.Messages` 才能看到完整 tool 配对，故校验必须是 **SmartCompressor 之后**的最后一个 BeforeModel 回调，直接对 `args.Request.Messages` 操作。
- **D3 修复只改本次发送，不动持久投影**：修复作用于 `args.Request.Messages`(瞬时)，**不回写 projection**。持久层的正确性由 D1(幂等)负责，避免修复产生持久副作用/与压缩打架。
- **D4 保守修复策略**：重复 tool 消息(同 tool_call_id)→ 保留首个、删其余；孤立 tool 消息(无前序匹配 tool_call)→ 补一个占位 assistant tool_call 使其配对合法(保留结果内容优于丢弃)；每次修复打 warn。
- **D5 错误分类（已撤销）**：原拟区分瞬时/永久错误做针对性重试。实现期核实 `RunFlow` swallow 模型错误（重试对其不触发），premise 失效 → 撤销，保持架构简单。
- **D6 抑制重复错误（已撤销）**：同上，无重试放大则无需抑制。诚实透出真实错误移交消费端。

## Risks / Trade-offs

- **改 Append 是全局不变量变更**：影响所有追加路径 + Compactor。风险：key==0 语义、Replace 后已见集合重建。缓解：仅对 key>0 去重；充分单测覆盖 Replace/压缩往返。
- **校验/修复误伤合法上下文**：过激修复丢失有效 tool 结果。缓解：D4 保守(优先补占位而非删除)；只处理可证明的孤立/重复；打 warn 可观测。
- **错误分类跨 provider 脆弱**：误判永久→少重试瞬时错误。缓解：D5 默认瞬时(偏保守重试)，永久判定需强特征。
- **集成点依赖框架 BeforeModel 顺序**：校验必须在压缩之后。风险：回调注册顺序错则校验的是未压缩列表。缓解：在 context_manager 注册处显式保证顺序 + 注释。
- **双投影真实来源未定**(开放问题)：D1 幂等能挡住入库，但若上游 onEvent 被调用两次，根因仍在上游。建议先 spike(warn 日志观测命中率/来源)再决定是否堵上游。

## Phase 0.2 结论（源头溯源）

静态追踪结论（`RunFlow` swallow 错误 + 三条投影追加路径核实后）：

- 重复的是**框架 tool 事件经 `onEvent`(context_manager:566) 双持久化**。按设计每类事件仅一条持久化路径（user→AppendEventHook；tool/LLM→eventCh onEvent），故重复是**不变量被违反**。
- **静态无法定位上游双发的确切触发**（框架 eventCh 重复投递 / MemoryPlugin 键分配 / 流式）——需一次运行时轨迹确认。
- 已加**判别性诊断**（session.go onEvent 边界记录 `evt.ID + event_key + role + type`）：下次复现即可判别两类根因——**同 `evt.ID`=框架重复投递**；**异 `evt.ID` 同 `event_key`=MemoryPlugin 键碰撞**（后者意味着 L1 会误删不同事件，需改判别策略）。
- 本次 `[122][123]` 内容/`tool_id` 完全相同 ⇒ **同一逻辑事件**（非键碰撞），故 **L1 按 key 幂等去重是安全且正确的**——它在 tagent 拥有的投影层强制"一个逻辑事件=一条投影"的不变量，中和任何上游双发。
- **决定**：L1（持久层幂等）作为 tagent 可控边界的正确治理点先落地；上游确切来源留待诊断日志的运行时轨迹，若确认为框架/插件缺陷再单独上游修复。**不在缺证据时臆造源头修复**。
