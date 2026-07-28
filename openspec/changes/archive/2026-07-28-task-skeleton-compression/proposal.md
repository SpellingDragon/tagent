# task-skeleton-compression

## Why

当前上下文压缩存在一个**结构性死锁**：`SegmentMessages` 以 `user` 消息为段边界，而定级规则（`deterministic-compress-level`）以 `HasUserInput` 为核心判据。由于每个段天然以 user 消息开头，`HasUserInput` 恒为真，导致：

- **L3 全归档成为死代码**：定级永远命中 `age<keepRecent*3 或 HasUserInput → L2`，L3 不可达。生产日志佐证：`l3_full` 恒为 0，`relations.journal` 中无任何 `archive/SetParent` 记录。
- **段数只增不减**：L1/L2 都"保留 user 消息"，`external_input` 的 ref 永远无法进入 rolling summary 归档，每轮新 user 输入又追加一个段。生产日志中段数单调增长 `L2: 12→25→34→46→51→56→61`（跨 3 天）。
- **代价放大**：projection 随段数膨胀，每次 `BeforeModel` 对所有 ref 全量 `GetEvent()` 解析（61 次 store 查询），压缩延迟随历史线性劣化。

根因是**段边界定义与归档判据耦合**：以 user 为界导致"每段都含 user"，以"含 user"为保留依据导致"无段可归档"。

## What Changes

将压缩的组织单元从"user 界定的碎片"重构为"agent_output 界定的完整任务回合"，并让压缩围绕**任务骨架**（`external_input` + `agent_output`）展开：

- **切段边界改为 `agent_output`**：一个段 = 一次完整任务回合 `[external_input, (thinking_plan|action_command)*, agent_output]`，以最终回复收尾，语义上对应"一个任务"。未闭合的尾部（无 `agent_output`）视为进行中段。
- **压缩丢弃按 `tool > assistant` 优先级**：段内中间事件先丢 `action_command`（工具执行痕迹），不足再丢 `thinking_plan`（中间思考），始终保留 `external_input` 原话与 `agent_output` 结论这两级骨架。
- **新增"多段压缩"作为归档出口**：当所有老段都已压到骨架（仅剩 `external_input` + `agent_output`）仍超预算时，将多个老骨架段从时间线移除，合并浓缩进 rolling summary（复用 `buildRetainedRefs` / `extractCardLine` 卡片机制，零 LLM 可走通）。这为 `external_input` ref 打通归档出口，段数随之下探，解开死锁。
- **重写定级规则**：以"agent_output 段 + 段龄"取代"user 段 + HasUserInput"，使归档级别真正可达。
- **`recentFullCount` 与 `keepRecent` 挂钩**（code review M2）：默认值取 `keepRecent × DefaultRefsPerTurn`，保证最近 `keepRecent` 个完整回合整体全量解析，避免次新 L0 回合被降级（见 design D6）。
- **巨段拆分纳入范围**（code review M3）：`generateSummaryRecursive` 的 `max_summary_input_chars` 巨段拆分（超限按消息组二分段递归摘要、单条超限 as-is 不截断）纳入本 change，作为 legacy 路径摘要输入拆分的配套；skeleton 路径纯 engineering 不受影响（见 design D7）。
- **BREAKING**：`TaskSegment` 的边界语义改变（user 界 → agent_output 界）；L0–L3 各级"保留/丢弃内容"重新定义（骨架优先）；`HasUserInput` 判据废弃。

## Capabilities

### New Capabilities
- `task-skeleton-compression`: 以 agent_output 为段边界、按 `tool > assistant` 优先级丢弃中间事件、保留 `external_input + agent_output` 任务骨架，并在骨架仍超预算时触发多段合并压缩（rolling summary 归档出口）的完整压缩能力。

### Modified Capabilities
- `deterministic-compress-level`: 定级规则重写——段边界由 user 改为 agent_output；级别语义由"L1 留 user+key / L2 留 user / L3 归档"改为"L0 完整 / L1 丢 tool / L2 丢 assistant(仅骨架) / 多段压缩归档"；废弃 `HasUserInput` 判据，使归档级别可达。

## Impact

- **代码**：
  - `agent/compress/task_segmenter.go`：`SegmentMessages` 边界判定（user → agent_output），需从消息识别"最终回复"（`agent_output` event type 前缀 / assistant 且无 tool_calls）。
  - `agent/compress/smart_compress.go`：定级函数重写；`selectiveSplit`（key/nonKey）替换为"骨架 vs 中间事件"二分 + `tool>assistant` 丢弃序；新增多段压缩路径衔接 rolling summary。
  - `agent/compress/context_compressor.go`：`buildRetainedRefs` 与 `extractCardLine` 已天然适配骨架模型（仅 `external_input`/`agent_output` 成卡片），需确认多段压缩段正确汇入。
- **规格**：`deterministic-compress-level`（重写）；`value-driven-compression` 的 L0–L3 保留/丢弃定义随骨架模型调整（其 LLM 评估路径当前未启用，后续对齐，本 change 不展开）。
- **行为**：老 `external_input` 段获得归档出口，段数与 projection 规模随历史增长被收敛；每次 `BeforeModel` 的 ref 解析规模随之下降。
- **性能（code review）**：`recentFullCount` 默认与 `keepRecent` 挂钩保证 L0 全量（D6）；预算升级 O(n²)→O(n) 优化列入交付任务（M1）。
- **范围（code review）**：巨段拆分（`max_summary_input_chars`）纳入本 change，仅服务 legacy 路径（D7）。
- **兼容性**：`compression-user-message` 的"保留 pending user / ensureUserPrompt"语义需在新骨架模型下重审（pending user 即进行中段的 `external_input`，天然保留）。
