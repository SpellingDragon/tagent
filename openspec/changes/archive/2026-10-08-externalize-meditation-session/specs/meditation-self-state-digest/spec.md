# meditation-self-state-digest Specification（MODIFIED delta）

## MODIFIED Requirements

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
