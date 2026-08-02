# task-skeleton-compression Delta

## ADDED Requirements

### Requirement: 纯工具调用 thinking_plan 生成工具调用摘要

`GenerateEventSummary` 对 `thinking_plan` 且 `Content` 为空且 `ToolCalls` 非空的事件，SHALL 用 ToolCalls 生成 `调用 <工具名>` 摘要（工程提取、零 LLM），使老化渲染不再退化为空摘要占位符。`Content` 非空的 thinking_plan SHALL 仍取原文。

#### Scenario: 纯工具调用生成摘要

- **GIVEN** 一个 thinking_plan 事件，`Content=""` 且 `ToolCalls=[read_file, grep]`
- **WHEN** 生成 EventSummary
- **THEN** SHALL 返回 `调用 read_file、grep`，而非空字符串

### Requirement: 老化工具运行折叠为工具链合成引用

骨架压缩 SHALL 把**连续的老化完整工具对**（thinking_plan + action_command 序列，中间不被 external_input/agent_output 打断，且处于 full=false 渲染区间）折叠为一个 `tool_chain` 合成引用（负 key），其 EventSummary 为一行工具链（`- 工具链: name1→name2→…（N步）[evt_first→evt_last]`）。原工具事件 refs SHALL 从投影移除、由该合成引用替代（无双重表示）。该合成引用 SHALL 使用独立的 `tool_chain` 事件类型（区别于 `context_compress`，不被吸收进滚动摘要计数），且 `buildRetainedRefs` SHALL 一律保留它。工具名 SHALL 取自 ref.EventSummary（无需回取全文）。

#### Scenario: 连续工具对折叠为单行工具链

- **GIVEN** 老化区间内有连续 3 对工具事件（read_file/grep/edit 及其结果）
- **WHEN** 执行压缩前的工具链折叠
- **THEN** SHALL 折叠为一个 `tool_chain` 合成引用，EventSummary 为 `- 工具链: read_file→grep→edit（3步）[evt_first→evt_last]`
- **AND** 原 6 条工具事件 refs SHALL 从投影移除

#### Scenario: 不跨回合边界折叠

- **GIVEN** 工具运行中间隔着一个 agent_output 边界事件
- **WHEN** 折叠
- **THEN** SHALL 在边界处断开，两侧分别折叠，不跨回合合并

### Requirement: 活跃前沿与近期配对不折叠

工具链折叠 SHALL 只作用于已老化的完整工具对；最近 `recentFullCount` 条（full=true）消息、当前进行中的未完成 tool_call（无 result）、以及边界事件 SHALL 不折叠、保持原生协议形态（tool_call 配对合法）。

#### Scenario: 活跃前沿保持原生

- **GIVEN** 进行中段的最近若干工具对处于 full=true 区间，且存在一个无 result 的未完成 tool_call
- **WHEN** 折叠
- **THEN** 这些消息 SHALL 保持原生（含 tool_calls），不被折叠
- **AND** 渲染后 tool_call 配对合法性保持

### Requirement: 上下文维护五项不变量

上下文维护 SHALL 满足：I1 有界（进行中段工具历史折叠为 O(工具链行数)，与循环长度解耦）；I2 稠密（无空摘要零信息占位符）；I3 锚定（滚动摘要常驻可见）；I4 无损（工具事件本体永在 MemoryStore，工具链行带票据可经 memory_turn 取回完整链）；I5 原生前沿（活跃前沿与近期配对保持原生）。

#### Scenario: 长进行中段上下文有界且无占位符

- **GIVEN** 一个 ~60 步工具调用的进行中段
- **WHEN** 渲染模型上下文
- **THEN** 工具历史 SHALL 收敛为少量工具链行（而非 ~130 条逐条消息）
- **AND** 上下文中 SHALL NOT 出现 `(历史事件摘要为空，可用 recall 检索)` 占位符
