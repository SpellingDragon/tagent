# event-timeline-rendering Specification

## Purpose

统一事件投影与时间线渲染：投影为 LLM 输入的唯一装配源；回合内同步工具交互以原生协议渲染，跨回合异步结果以通知类输入事件渲染；看板为 user 级虚事件；输出侧协议卫生与渲染合法性不变量。

## Requirements

### Requirement: 投影为 LLM 输入的唯一装配源

每次 BeforeModel 组装发送给模型的消息时，系统 SHALL 仅以 `[system 消息] + render(投影) + 任务看板` 组装请求。系统 SHALL NOT 读取框架 `args.Request.Messages` 中除 system 消息以外的任何内容（不读回框架维护的消息尾部、不按字符串前缀启发式区分消息来源）。事件数据流 SHALL 为单向：事件流 → 投影 → 请求渲染。

#### Scenario: 装配不依赖框架消息尾部

- **WHEN** 框架 `args.Request.Messages` 的非 system 部分包含任意（包括损坏/重复/伪造的）消息
- **THEN** BeforeModel 组装出的最终消息序列 SHALL 与不包含这些消息时完全一致

#### Scenario: 正常 ReAct 迭代的装配

- **WHEN** 一个 ReAct 回合中第 K 次迭代触发 BeforeModel
- **THEN** 最终消息 = `[system] + render(投影全部引用) + 任务看板(如有活动任务)`
- **AND** 序列中 SHALL NOT 出现来自框架消息尾部的额外副本

### Requirement: 投影写入与事件存储同点同步

每个被存储到 MemoryStore 的框架事件 SHALL 在同一同步点（事件插件管线内）被追加到当前 invocation 的投影，且恰好一次；未被存储的事件 SHALL NOT 进入投影。投影 SHALL 是事件存储的忠实索引（stored ⟺ projected）。不同 invocation（主循环 / sub-agent）SHALL 写入各自的投影，互不错配。

#### Scenario: 工具调用与结果逐一入投影

- **WHEN** 一个 ReAct 回合依次产生 assistant(tool_calls)、tool result、assistant(final) 事件
- **THEN** 每个事件在插件管线处理完成时即出现在投影中
- **AND** 投影中每个 EventKey 恰好出现一次

#### Scenario: sub-agent 与主循环投影隔离

- **WHEN** sub-agent 调用与主循环并发处理各自的事件
- **THEN** 各自事件 SHALL 只写入各自 invocation 的投影

### Requirement: BeforeModel 时投影完整性为构造保证

对任意第 K 次迭代的 BeforeModel，投影 SHALL 已包含此前所有已存储事件（含第 K-1 迭代的 assistant 与 tool result）。该完整性 SHALL 由机制保证（框架对工具结果事件的 completion-wait 覆盖插件处理；runner FIFO 处理顺序的传递性），SHALL NOT 依赖 goroutine 调度的时序巧合。流式场景下，partial（IsPartial）事件 SHALL NOT 被存储或投影，仅聚合事件进入投影。

#### Scenario: 工具结果在下一次 BeforeModel 前已在投影

- **WHEN** 第 K 次迭代的工具执行完成并发出 tool result 事件
- **THEN** 第 K+1 次迭代 BeforeModel 渲染的投影 SHALL 包含该 tool result

#### Scenario: partial 事件不入投影

- **WHEN** 流式模式下产生 IsPartial 的增量事件
- **THEN** 这些事件 SHALL NOT 被存储、SHALL NOT 出现在投影中

### Requirement: 回合内原生、跨回合通知的时间线渲染

投影渲染为模型消息时，回合内同步工具交互 SHALL 以原生协议形态呈现，跨回合异步结果 SHALL 以通知类输入事件呈现：

- `thinking_plan` → role=assistant，携带原生 ToolCalls（自 FullEvent.ToolCalls 还原）；content 中 SHALL NOT 生成任何文本化调用语法（文本调用语法可被模型模仿产生伪调用）
- `action_command`（有 ToolID 且其 tool_call 在渲染序列中）→ role=tool + ToolID（原生配对）
- `action_command`（无 ToolID，或其 tool_call 已被压缩/缺失）→ 降级为 role=user 输入注记，内容 SHALL 保留（含关联标识）
- `external_input`（含 task/monitor/meditation 通知）→ role=user 文本，通知携带文本级关联标识（如 task id）
- `agent_output` → role=assistant 文本；`context_compress` → role=system 文本

#### Scenario: 工具交互历史以原生协议呈现

- **WHEN** 投影包含 thinking_plan（带 tool_calls）+ action_command（其结果，有 ToolID）
- **THEN** 渲染序列中 thinking_plan 为 assistant 消息且携带原生 ToolCalls，action_command 为 role=tool 消息且 ToolID 与之配对
- **AND** assistant 消息的 content 中 SHALL NOT 出现系统生成的文本调用语法

#### Scenario: 压缩切窗后残余结果降级不丢内容

- **WHEN** 压缩将某 tool_call 所在的窗口替换为摘要，而其结果事件仍在保留窗口
- **THEN** 该结果 SHALL 降级为 role=user 输入注记（内容与关联标识保留）
- **AND** 渲染结果仍为合法原生序列（无孤儿 role=tool）

### Requirement: 任务看板为 user 级独立虚事件

任务看板 SHALL 作为独立的 role=user 虚事件注入（非合并、非 system、非 assistant），文本 SHALL 声明其为系统注入的观察快照且不应被模仿；看板 SHALL NOT 进入投影/存储。

#### Scenario: 看板以虚事件形态注入

- **WHEN** 存在活跃异步任务时装配请求
- **THEN** 看板为独立的 role=user 消息，含“系统注入的观察快照…勿模仿”声明
- **AND** 看板内容不出现在投影或 MemoryStore 中

### Requirement: 输出侧协议卫生

模型伪造的协议痕迹（伪 `[evt_…]` 前缀、伪系统注记/文本化调用行）SHALL 在存储边界与投递边界被检测并剥离；剥离后为空的输出 SHALL 按退化空响应处理（不存储、不投递、触发一次重试）。

#### Scenario: 伪造痕迹被双边界清理

- **WHEN** 模型输出包含伪造的 [evt_…] 前缀或文本化调用行
- **THEN** 存储的 FullEvent.Content 与投递给消费端的 content 均不含该痕迹
- **AND** 若剥离后为空，该回合按退化空 turn 处理并重试一次

### Requirement: 渲染合法性不变量

渲染输出 SHALL 满足：恒为合法原生序列（每个 role=tool 消息均有前序声明其 ToolID 的 assistant tool_call，无孤儿/无重复应答）；无空内容的纯文本 assistant 消息；同一 EventKey 不重复出现。系统 SHALL 提供测试态断言以锁定该不变量。

#### Scenario: 渲染结果通过合法性断言

- **WHEN** 对任意投影执行渲染
- **THEN** 输出 SHALL 满足全部渲染合法性不变量
