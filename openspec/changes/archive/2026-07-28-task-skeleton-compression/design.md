# task-skeleton-compression — 技术设计

## Context

当前压缩管线的组织单元是 `SegmentMessages` 以 `user` 消息切出的"碎片段"，定级依赖 `HasUserInput`。由于每个段天然含 user，归档级别（L3）不可达，`external_input` ref 无归档出口，段数与 projection 规模随历史单调膨胀（生产实证 `L2: 12→61`）。

系统已存在一条与本次设计天然同构的机制：`ContextCompressor.buildRetainedRefs` 把"key 不在压缩结果里"的 ref 收进 rolling summary，而 `extractCardLine` **只对 `external_input` 与 `agent_output` 生成任务卡片**。本设计把"任务骨架"从卡片提取的副产品，上升为切段与压缩的组织原则。

相关方：`SmartCompressor`（切段/定级/丢弃）、`ContextCompressor`（ref 解析/rolling summary 收编）、`deterministic-compress-level`（定级规格）。

## Goals / Non-Goals

**Goals:**
- 段边界改为 `agent_output`：段 = 完整任务回合 `[external_input, (thinking_plan|action_command)*, agent_output]`。
- 压缩丢弃按 `tool(action_command) > assistant(thinking_plan)` 优先级，保留 `external_input` 原话与 `agent_output` 结论两级骨架。
- 新增"多段压缩"：骨架段仍超预算时，老骨架段汇入 rolling summary，打通 `external_input` 归档出口，解开死锁。
- 重写定级为"agent_output 段 + 段龄"纯函数，使归档级别可达；零 LLM 可走通（engineering 卡片兜底）。

**Non-Goals:**
- 不改 rolling summary ref 的持久化格式（`buildRetainedRefs` 输出结构不变）。
- 不对齐 `value-driven-compression` 的 LLM `EventValuator` 路径（当前未启用，留后续）。
- 不处理 `compression-user-message` 中"摘要 System role vs User role"的 spec/代码漂移（仅标注，另行对齐）。
- 不引入新的 MemoryStore schema 或迁移。

## Decisions

### D1：段边界用 `agent_output` 事件类型识别，启发式兜底

段的"闭合"由 `agent_output` 触发。识别优先级：

1. **主判据 — event 前缀**：`resolveRef` 已给每条消息打上 `[evt_KEY|type]` 前缀（见 `prefixEventKey`），`agent_output` 类型可直接经 `ParseEventKeyAndType` 精确识别。
2. **兜底启发式**：对缺失前缀的输入（如单测直接构造 `[]model.Message`），以 `assistant 且 len(ToolCalls)==0` 视为回合收尾。

*为什么不用 role 而用 event type*：`agent_output` 与 `thinking_plan` 在 message 层同为 `assistant`，role 无法区分；event type 前缀是唯一可靠信号。启发式仅为兼容裸 message 输入，不承担主路径。

*分段规则*：顺序扫描，遇 `agent_output` 闭合当前段；连续的 `external_input`（用户连发、agent 未回）归入同一进行中段；无 `agent_output` 的尾部为**进行中段**（`IsComplete=false`），永不归档、始终保留以驱动 LLM。

### D2：丢弃序 `tool > assistant`，骨架二分取代 key/nonKey

段内事件二分：

- **骨架**（保留）：`external_input`（user 原话）、`agent_output`（最终结论）。
- **中间事件**（可弃）：`action_command`（tool 执行痕迹）、`thinking_plan`（assistant 中间过程）。

丢弃按优先级分两档：`action_command` 先于 `thinking_plan`。这替代现有 `selectiveSplit` 的 key/nonKey 启发式（基于内容长度/含 "Error" 等脆弱规则）——骨架判定是**事件类型的纯函数**，不读内容，更确定。

*为什么 tool 先于 assistant*：工具结果是体积最大、复用价值最低的部分（大段命令输出/文件内容）；`thinking_plan` 体积较小且承载推理脉络，在 tool 已弃、仍需再减时才弃。这与"尽量保留"用户原话与结论的目标一致。

### D3：定级 = agent_output 段龄的纯函数，归档级别可达

新级别语义（`age = 段在新→旧序列中的位置`，0=最新）：

| 级别 | 触发 | 段内保留 | 段内丢弃 |
|------|------|---------|---------|
| L0 | `age < keepRecent` | 全部 | 无 |
| L1 | `age < keepRecent*2` | 骨架 + `thinking_plan` | `action_command` |
| L2 | `age < keepRecent*3` | 骨架 | `action_command` + `thinking_plan` |
| 多段压缩 | 更老 或 预算仍不足 | （整段移出时间线） | 全段 → rolling summary |

`HasUserInput` 判据废弃。段边界不再以 user 定义，归档不再被"每段都含 user"堵死——**这是解开死锁的关键**。

### D4：多段压缩复用 rolling summary，零 LLM 兜底

多段压缩不在 `SmartCompressor` 内新造归档格式，而是**让被压骨架段的 key 不进入 `compressedMsgs`**，由 `ContextCompressor.buildRetainedRefs` 自然收编：

- `extractCardLine` 已为 `external_input`/`agent_output` 生成 80 字卡片 → 骨架信息进入 rolling summary，recall 仍可溯源。
- 无摘要模型时纯 engineering 走通（`curateCards` 超限 sink 到 earlier 计数）；有摘要模型时可选 LLM 浓缩骨架段（增强，非必需）。
- rolling summary 有 `listedKeysCap` + `cardMaxChars` + sink 三重界，天然不膨胀。

*为什么不走 `archiveSegment`/L3*：那条路径依赖摘要模型且当前是死代码；rolling summary 是已验证、已持久化、零 LLM 可用的现成出口。

### D5：压缩产物仍以 event key 前缀衔接 buildRetainedRefs

`SmartCompressor` 输出的 `compressedMsgs` 中，被保留的骨架/中间消息**保留原 event key 前缀**。`buildRetainedRefs` 据此判定"哪些 ref 存活"，存活的留 projection，未存活的（被丢的中间事件 + 被多段压缩的整段）汇入 rolling summary。衔接面不变，改动收敛在 `SmartCompressor` 内部。

### D6：`recentFullCount` 与 `keepRecent` 挂钩，保证 L0 回合整体全量（code review M2，已决）

`resolveRef` 按 ref 索引后缀切全量解析界（`fullFrom = len(refs) - recentFullCount`）。若 `recentFullCount` 取固定小值（默认 4），而 `keepRecent=2`、单回合含一次工具调用即占 4 个 ref（`external_input`/`thinking_plan`/`action_command`/`agent_output`），则仅**最新 1 个回合**落全量区，**次新的 L0 回合（age=1）落到 summary-only 区**，其 `action_command` 被 `demoteToInputNote` 降级——削弱“L0=全保真”语义。

**决策**：`recentFullCount` 未显式配置时，默认值与 `keepRecent` 挂钩，取 `keepRecent × DefaultRefsPerTurn`（`DefaultRefsPerTurn=4`），使最近 `keepRecent` 个完整回合整体落全量区。显式配置（`WithRecentFullCount` / `recent_full_count`）优先于该推导值。

*正确性论证*：full 区恒为后缀、`action_command`（result）恒在其 `thinking_plan`（call）之后，故无论切界如何都**不产生 dangling tool_call**——此决策仅提升保真度，不改变协议合法性。

### D7：巨段拆分（`MaxSummaryInputChars`）纳入本 change 范围（code review M3，已决）

`generateSummaryRecursive` 的巨段拆分（`max_summary_input_chars`：超限按消息组二分段递归摘要、单条超限 as-is 不截断）是先前会话的并行工作，被一并带入工作区。**决策**：纳入本 change，作为 legacy 路径（`compressLegacy`）摘要输入拆分的配套能力。注意 skeleton 路径（`compressSkeleton`）为纯 engineering、不调 LLM，巨段拆分仅服务 legacy 路径。要求补单测覆盖“超限触发拆分”与“单条不截断”两条路径。

## Risks / Trade-offs

- **`agent_output` 识别失败致段不闭合**（缺前缀 + 启发式误判）→ 段过大、压缩粒度变粗。→ 缓解：event 前缀为主判据，启发式仅兜底；识别失败时退回"按 `external_input` 软分段"保底；单测覆盖连发输入/无输出尾部。
- **骨架总量本身超预算**（回合极多，`external_input`+`agent_output` 已超限）→ 多段压缩后 rolling summary 变长。→ 缓解：rolling summary 自带 `listedKeysCap`/`cardMaxChars`/sink 上界；`keepRecent` 可调以控制保留回合数。
- **用户原话归档后意图只剩卡片**（80 字）→ 老任务细节依赖 recall。→ 缓解：这是用户已确认的取舍；"尽量保留"由骨架模型本身保证（`external_input` 直到多段压缩才归档）+ `keepRecent` 调大保留更多完整回合；recall 可取全文。
- **段变大使单次 `resolveRef`/`GetEvent` 更集中** → 老 `agent_output` 段仍逐 ref 全量解析。→ 缓解：本 change 将 `recentFullCount` 接入 `resolveRef`（老 ref 用 `EventSummary` 替代全量 `GetEvent`），收敛每次 `BeforeModel` 的 store 查询规模。
- **`recentFullCount` 按 ref 索引切界可能腰斩 L0 保真**（code review M2）→ 次新 L0 回合落 summary-only，`action_command` 被降级。→ 缓解：见 D6，`recentFullCount` 默认与 `keepRecent` 挂钩（`keepRecent × DefaultRefsPerTurn`），使 L0 回合整体全量；正确性不受影响（full 区为后缀，不产生 dangling call）。
- **预算升级为 O(n²)**（code review M1）→ `compressSkeleton` L3 升级循环每步重算全量 `estimate()`。→ 缓解：稳态段数≈8（设计已收敛），O(n²)≈64 可忽略；61 段为旧模型迁移的一次性 backlog。优化方向：预计算 `cost[i][0..3]` 后 O(1) 增量升级，降为 O(n)（见 tasks 6.1）。

## Migration Plan

1. **纯内存行为变更**，无持久化 schema 改动：rolling summary ref 格式不变；旧 projection 中的历史消息在每次 `BeforeModel` 重新 resolve + 切段，天然切换到新边界。
2. 灰度：`NewSmartCompressor` 增加开关（如 `WithSkeletonSegmentation(true)`，默认随版本开启），可按配置回退到旧 user 切段。
3. 回滚：关闭开关即恢复旧 `SegmentMessages` 边界 + 旧定级；无数据迁移。
4. 验证：先以生产日志同款 session 回放，确认段数随轮次收敛、`l3/多段压缩` 级别真实触发、`external_input` 进入 rolling summary。

## Open Questions

- `keepRecent` 默认值是否随“段=完整回合”下调（回合含中间事件，单回合 token 高于旧碎片段）？倾向保持 2，待回放数据校准。
- 多段压缩在有摘要模型时是否启用 LLM 浓缩骨架段，还是一律走 engineering 卡片？倾向先一律 engineering（零 LLM 可回归），LLM 浓缩作为后续增强。
- 进行中段的 `external_input` 是否参与任何压缩？倾向永不压缩（它是驱动 LLM 的 pending 输入，与 `compression-user-message` 的 pending user 语义一致）。

### 已决（code review）

- **M2 `recentFullCount` 语义**：已决，与 `keepRecent` 挂钩（见 D6），保证 L0 回合整体全量。
- **M3 巨段拆分归位**：已决，纳入本 change 范围并补测（见 D7）。
