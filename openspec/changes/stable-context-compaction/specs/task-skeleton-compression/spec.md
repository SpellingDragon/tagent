## REMOVED Requirements

### Requirement: 压缩触发器多维化（token 阈值或完整段超龄）

**Reason**: 稳态下轮数维度每轮触发整理路径（稳态保留 ~4×keepRecent 个完整回合恒大于 keepRecent），叠加每轮工具链折叠与滑动全文窗口，使投影渲染每轮变化、LLM 前缀缓存持续失效。整理应当只由真实容量压力触发；`keep_recent_tasks` 等参数指引的是**整理后的状态**，而非整理触发。原"防饿死"动机（占位符渲染低估 token）已被工具调用摘要与工具链折叠根治——渲染有信息量，token 估算不失真；token 随 L2 骨架驻留累积终将触达阈值，整理不会饿死。

**Migration**: 触发判断收敛为 `usedTokens > compress_threshold × max_tokens` 单维。依赖轮数触发的既有测试迁移为容量触发等价断言（低预算强制触发即可覆盖原场景）。小内容长会话的 refs 缓慢增长为接受的行为变化（无容量压力时保持原文即最高保真）。

## ADDED Requirements

### Requirement: 压缩触发器单维化（token 容量阈值）

`ContextCompressor.Compress` SHALL 以**单一维度**触发整理：仅当 `usedTokens > compress_threshold × max_tokens` 时才调用 `SmartCompressor.Compress` 与投影重写；未超阈值时 SHALL pass-through——直接返回按投影渲染的消息且**不触碰投影**（不折叠、不定级、不重建 refs）。完整回合计数 SHALL NOT 参与触发判断。

#### Scenario: 未超阈值时完全直通

- **GIVEN** 会话已有远超 `keep_recent_tasks` 的完整回合数，但 `usedTokens` 低于阈值
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL NOT 调用 `SmartCompressor.Compress`，SHALL NOT 执行工具链折叠，投影 refs SHALL 原样返回

#### Scenario: 超阈值触发整理（回归）

- **GIVEN** `usedTokens` 超过 `compress_threshold × max_tokens`
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL 调用 `SmartCompressor.Compress`（与既有 token 触发行为一致）

### Requirement: 整理间渲染冻结（前缀字节稳定）

整理（容量触发的压缩轮）之间，投影渲染 SHALL 是 **append-only 稳定**的：全文解析窗口（`recent_full_count`）SHALL 在整理轮锚定为投影内的边界 key（最近 `recent_full_count` 条 retained refs 的最前一条），整理间该边界 SHALL NOT 随 refs 追加而移动。整理后新增事件 SHALL 全文渲染（活跃前沿），既有 refs 的渲染方式（全文/摘要）SHALL 冻结不变——连续未触发轮发出的消息序列 SHALL 保持公共前缀字节级相同。

#### Scenario: 整理间追加新事件不改变旧渲染

- **GIVEN** 上次整理后投影含 refs R1..R50，其中 R35..R50 为全文窗口
- **WHEN** 新事件 R51..R53 追加且未触发整理，渲染下一轮请求
- **THEN** R1..R50 的渲染输出 SHALL 与上一轮逐字节相同（R35..R50 仍全文，R1..R34 仍摘要）
- **AND** R51..R53 SHALL 全文渲染（活跃前沿）

#### Scenario: 整理轮重设边界

- **GIVEN** token 超阈值触发整理
- **WHEN** `buildRetainedRefs` 产出新的 retained refs
- **THEN** 全文窗口边界 SHALL 重设为最近 `recent_full_count` 条 retained refs 的最前一条
- **AND** 之后整理间的渲染方式以新边界冻结

## MODIFIED Requirements

### Requirement: 老化工具运行折叠为工具链合成引用

骨架压缩 SHALL 在**整理轮**（容量触发的压缩路径内）把**连续的老化完整工具对**（thinking_plan + action_command 序列，中间不被 external_input/agent_output 打断，且处于摘要渲染区间）折叠为一个 `tool_chain` 合成引用（负 key），其 EventSummary 为一行工具链（`- 工具链: name1→name2→…（N步）[evt_first→evt_last]`）。原工具事件 refs SHALL 从投影移除、由该合成引用替代（无双重表示）。该合成引用 SHALL 使用独立的 `tool_chain` 事件类型（区别于 `context_compress`，不被吸收进滚动摘要计数），且 `buildRetainedRefs` SHALL 一律保留它。工具名 SHALL 取自 ref.EventSummary（无需回取全文）。整理间（未触发轮）SHALL NOT 执行折叠——老化区间外的工具对按 EventSummary 渲染（有界且字节稳定），待下次整理统一折叠。

#### Scenario: 连续工具对折叠为单行工具链

- **GIVEN** 整理轮的老化区间内有连续 3 对工具事件（read_file/grep/edit 及其结果）
- **WHEN** 执行整理路径内的工具链折叠
- **THEN** SHALL 折叠为一个 `tool_chain` 合成引用，EventSummary 为 `- 工具链: read_file→grep→edit（3步）[evt_first→evt_last]`
- **AND** 原 6 条工具事件 refs SHALL 从投影移除

#### Scenario: 不跨回合边界折叠

- **GIVEN** 工具运行中间隔着一个 agent_output 边界事件
- **WHEN** 折叠
- **THEN** SHALL 在边界处断开，两侧分别折叠，不跨回合合并

#### Scenario: 未触发轮不折叠

- **GIVEN** 摘要区间内有连续工具对，但 `usedTokens` 低于阈值（未触发整理）
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** 工具对 SHALL 保持原生 refs（按 EventSummary 渲染），SHALL NOT 产生或扩展 `tool_chain` 合成引用
