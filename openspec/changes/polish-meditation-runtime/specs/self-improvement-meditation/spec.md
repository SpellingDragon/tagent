# self-improvement-meditation Specification（delta）

## MODIFIED Requirements

### Requirement: 负反馈回顾(反思素材)

冥想回顾清单 MUST 包含 feedback 事件(任务失败 negative/用户不满)——失败教训是最高价值反思素材;§2 分析提示应将 negative 归因到痛点。

#### Scenario: 负反馈进反思
- **WHEN** 冥想触发且回顾近期事件
- **THEN** 反思清单覆盖 feedback 事件(negative 优先归因)——负反馈→冥想→改进的闭环闭合

反思 digest SHALL 携带让位欠账：自上次冥想执行以来的 deferred 次数与最近一次让位时刻，渲染为 digest 中的一行——模型据此在卡片中交代未兑现的反思及其成因；该计数为进程内会话语义（consumed 清零、重启归零），MUST NOT 引入新的持久化。

#### Scenario: 欠账可见

- **WHEN** 注入或消费时刻发生让位后，下一次冥想执行构建 digest
- **THEN** digest 含「让位 N 次」计数行，模型卡片可引用该欠账交代覆盖范围

