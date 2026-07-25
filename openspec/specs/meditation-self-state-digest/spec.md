# meditation-self-state-digest Specification

## Purpose
TBD - created by archiving change meditation-introspection-digest. Update Purpose after archive.
## Requirements
### Requirement: 冥想事件携带自我状态 digest

当一次冥想有效触发（空闲 ≥ `MinGap`）时，注入的冥想 `external_input` 事件 SHALL 在既有冥想 prompt **之前**前置一段**确定性生成**的"自我状态 digest"。digest SHALL 不调用 LLM、SHALL NOT 阻塞、SHALL 仅由当时的运行态快照渲染。

#### Scenario: 冥想消息包含 digest 段

- **WHEN** 一次冥想有效触发且已接入任务层
- **THEN** 冥想消息 SHALL 依次包含 `[meditation]` 头、自我状态 digest、原冥想 prompt
- **AND** digest SHALL 位于 prompt 之前

#### Scenario: digest 生成不依赖 LLM 且不阻塞

- **WHEN** 构建冥想消息
- **THEN** digest SHALL 由纯函数从运行态快照确定性渲染
- **AND** SHALL NOT 触发任何 LLM 调用或网络请求

### Requirement: digest 覆盖任务层健康与空闲时长

digest SHALL 至少包含：(a) 任务层按状态计数（running/stable/alive-detached/suspect/dead/failed 等）；(b) 需关注任务（`suspect`/`dead`/`failed`）的简摘（描述 + 状态 + 年龄）；(c) 距最近一次 agent 输出的空闲时长。任务数据 SHALL 只读获取（`TaskController.List()`），SHALL NOT 修改任务层。

#### Scenario: 存在卡死/死亡任务时列出简摘

- **WHEN** 任务层存在处于 `suspect` 或 `dead` 的任务
- **THEN** digest SHALL 列出这些任务的描述、状态与年龄
- **AND** SHALL 给出各状态的计数

#### Scenario: 空闲时长基于 agent 输出锚定

- **WHEN** 渲染 digest
- **THEN** 空闲时长 SHALL 以距最近一次 agent 输出（final response）的间隔计算

### Requirement: digest 有界渲染

digest SHALL 有界：逐条列出的任务明细 SHALL 有上限，超出部分 SHALL 以计数汇总而非逐条展开，避免冥想消息随任务规模无界膨胀。

#### Scenario: 任务过多时截断为汇总

- **WHEN** 需关注任务数超过明细上限
- **THEN** digest SHALL 只逐条展示上限内的条目
- **AND** 其余 SHALL 以计数形式汇总

### Requirement: 无任务层时优雅降级

当 `MeditationManager` 未接入 `TaskController`（未挂任务层）或活跃任务为空时，digest SHALL 优雅降级——省略任务明细或渲染为空/单行"无活跃任务"，且冥想的其余行为（节拍、`MinGap` 判定、prompt 注入）SHALL 与未引入 digest 前完全一致。

#### Scenario: 未接任务层时冥想行为不变

- **WHEN** 未注入 `TaskController` 便触发冥想
- **THEN** 冥想消息 SHALL 不含任务明细段
- **AND** 其触发条件与 prompt 注入 SHALL 与现状等价


### Requirement: 冥想总结以高亮卡片行沉淀

冥想 turn 的总结 SHALL 在固化时以高亮卡片行（★ 前缀）写入卡片序列（零 LLM 成本）,使周期性回顾沉淀为长期记忆;超限整理时其要点 SHALL 被浓缩保留。

#### Scenario: 冥想结论沉淀

- **WHEN** 冥想总结产出后发生 Compact
- **THEN** 卡片序列 SHALL 含该冥想的高亮行;原冥想事件仍照常存储/投影（不改变现有事件流）
