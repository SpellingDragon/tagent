# compress-digest-reconnect

## Why

生产实证（wechat-bot 2026-08-01）：上下文压缩**从未真正运行**。日志显示 `[ContextCompressor] under budget (37k <= 102400), 425 refs, 425 messages`——refs 与 messages 恒为 1:1、且随会话持续增长，无任何折叠。根因是一个**自我抑制**的结构：

1. `resolveRef` 为限制每轮 store 查询，只让最近 `recentFullCount` 个 ref 取全文，更老的 ref 直接用 `EventSummary` 渲染（空则占位符 `(历史事件摘要为空，可用 recall 检索)`）。这个**性能优化顺带把 token 压得很低**（占位符仅 ~20 字符）。
2. 而 `ContextCompressor.Compress` 的触发条件**只看 token 阈值**（`usedTokens > threshold`）——占位符让 `usedTokens` 永远到不了阈值，于是 `SmartCompressor.Compress` **从未被调用**。
3. 结果：骨架压缩（L1 丢 tool / L2 仅骨架 / L3 LLM 摘要）、滚动摘要、卡片序列**全部从未发生**；tool 事件永久滞留为"内容已丢、工具配对已断、却仍占消息槽"的无用占位符。

与此同时，**LLM 摘要的基础设施其实已建成且已接线**：`summary_model` 已在 tagent.yaml 配置（deepseek-v4-flash），L3 LLM 摘要带 `segmentContentHash` 归档缓存（内容相同复用摘要=素材律），`condenseCardLines` 也在 `curateCards` 里。但它们同样被触发器饿死——"重新接线 LLM 摘要"的本质是**先让压缩管线活过来**。

另外两个伴随问题：

- **执行过程召回断链**：骨架只把边界事件（external_input 意图 + agent_output 结果）内联成卡片，而"怎么做的"（thinking_plan/action_command）被 L1 丢弃后，模型**没有**拿回它们的手段——被丢弃事件的 key 不在卡片里，唯一的召回路径（从 agent_output 沿因果链 `GetParent` 回走）没有工具暴露。
- **死代码与噪声**：纯工具调用的 thinking_plan 事件 `EventSummary` 为空 → 占位符噪声；压缩路径需做一次死代码清理（`compressLegacy` 是有意的回退路径，**保留**，不属于死代码）。

## What Changes

1. **触发器多维化（P0）**：`ContextCompressor` 在 token 阈值之外，增加**"完整任务段超过 keepRecent"**这一廉价触发维度（复用现有 ref 遍历统计 agent_output 段边界数，零额外成本）。由此骨架压缩在"有料可压"时即运行——L1 丢 tool、L2 骨架、L3 LLM 摘要真实执行，滚动摘要与卡片序列开始形成，refs 不再无界增长。
2. **LLM 摘要接线对齐**：触发后让 L3 LLM 摘要（含归档缓存的素材律）与 `condenseCardLines` 真实运行并汇入滚动摘要；对齐"**边界卡片为基座（what/result 内联、票据保留）、LLM 文摘为可选叠加层（只读卡片生成、票据仍内联）**"的摄取模型。
3. **recall_turn 链召回**：新增 `memory_turn` 召回工具——给定一个事件 key（通常为 agent_output 卡片），沿 `GetParent` 回走因果链至最近的 `external_input` 为止，一次性返回该轮被丢弃的执行过程（thinking_plan / action_command）。配套给卡片行标注"含 N 步工具调用，可用 memory_turn 追溯"的可追溯提示，解决"how 怎么找回"。
4. **死代码清理**：清理压缩路径上的死代码；保留 `compressLegacy`（有意的回退路径，`skeleton_segmentation: false` 时可达）。
5. **测试设计**：触发器（段数/段龄触发）、recall_turn（边界回走）、L3 摘要 + 归档缓存（素材律）、condenseCards 文摘、端到端压缩状态机。

不改：骨架压缩的段边界（agent_output）、L0-L3 定级纯函数、tool>assistant 丢弃序、`compressLegacy` 回退路径、卡片行的票据语义。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `task-skeleton-compression`：触发器从"仅 token 阈值"扩展为"token 阈值或完整段超龄"；LLM 摘要（L3 归档 + condenseCards）在触发后真实运行并对齐"卡片为基、文摘为叠加"。
- `recall-protocol`：新增 `memory_turn` 因果链召回工具（边界锚定 + 回走重建执行过程）与卡片可追溯提示。

## Impact

- **代码**：`agent/compress/context_compressor.go`（触发门）、`agent/compress/smart_compress.go`（L3 摘要接线核对）、`tool/recall/`（memory_turn 工具）、卡片行标注。
- **行为**：骨架压缩首次真实运行——tool 事件按 L1 丢弃、滚动摘要与卡片序列形成、refs 停止无界增长；LLM 摘要真实生成（受归档缓存约束）；模型可经 `memory_turn` 找回任意轮次的执行过程。
- **风险面**：压缩首次真实运行意味着滚动摘要/卡片首次形成——需观察其形态是否符合摄取预期（what/result 内联 + how 可召回）；LLM 摘要引入模型调用（有缓存摊销）。
- **测试**：新增触发、召回、摘要、端到端测试；全量测试需保持绿。
