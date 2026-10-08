# meditation-self-state-digest Specification

## Purpose
冥想自我状态 digest：有效冥想触发时在 prompt 前前置确定性运行态快照，零 LLM、不阻塞。
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

digest 的覆盖面按形态区分：**in-loop 形态**（未配置观察面）SHALL 沿用既有覆盖——(a) 任务层按状态计数；(b) 需关注任务（`suspect`/`dead`/`failed`）简摘；(c) 距最近一次 agent 输出的空闲时长。**外部观察形态**（观察面非空）digest SHALL 以**被观察分区概况**为主：各观察分区自上次冥想以来的事件计数（分谱系汇总：非自管/自管）、最近非自管活动摘要；自身任务板明细 SHALL 可省略（外部观察者自身任务层通常为空，沿用"无任务层优雅降级"）。两形态的 digest 均 SHALL 确定性生成、零 LLM、不阻塞、有界渲染。

#### Scenario: in-loop 形态覆盖任务层健康

- **WHEN** in-loop 形态冥想触发且已接入任务层
- **THEN** digest 含任务状态计数与需关注任务简摘（既有语义不变）

#### Scenario: 外部形态覆盖分区概况

- **WHEN** 外部观察形态冥想触发
- **THEN** digest 含各观察分区自上次冥想以来的分谱系事件计数与最近活动摘要

#### Scenario: 外部形态自身无任务不报错

- **WHEN** 外部观察形态下冥想 agent 自身任务层为空
- **THEN** digest 省略自身任务段，分区概况照常渲染（优雅降级沿用）

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

