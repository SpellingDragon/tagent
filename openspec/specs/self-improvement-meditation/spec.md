# self-improvement-meditation Specification

## Purpose

冥想是一次事件驱动的反思：由自持状态机在真实 `external_input` 激活下触发（无 ticker、无独立门控能力），由配置冥想插件的 agent（缺省宿主自身，专属反思会话如 `tagent-session:tagent:reflection`）执行一个正常 turn——读自查摘要与观察分区的只读事实正文，自主判断本窗口是否存在值得业务方改变做法的结论；有结论时写成简短中文结论并仅经 `send` 跨会话回流到目标会话，无结论则不产出正文、不调用工具。插件按三挂点形态挂载（标准工具面、事实链、插件自有状态机），不引入冥想专属运行时机制。
## Requirements
### Requirement: 冥想为事件驱动的反思回合

冥想 SHALL 由事件驱动的自持状态机在真实 `external_input` 激活下触发（无 ticker、无独立门控能力），由配置冥想插件的 agent（缺省宿主自身，专属反思会话如 `tagent-session:tagent:reflection`）执行一个正常 turn：读自查摘要与观察分区的只读事实正文，自主判断本窗口是否存在值得业务方改变做法的结论；有结论时写成简短中文结论并仅经 `send` 跨会话回流到部署侧圈定的目标会话（越界目标由框架门控拒收），无结论则不产出正文、不调用工具。独立策展 agent 是显式声明的跨域选项（同一机制、不同观察面与反思会话）。状态激活、自管产出不计入状态积累与五不变量见 `meditation-autonomy`；插件按三挂点形态挂载（标准工具面、事实链、插件自有状态机），不引入冥想专属运行时机制。

#### Scenario: 叙事主语统一

- **WHEN** 核对 wiki/README 的冥想叙述
- **THEN** 反思主体表述为「配置冥想插件的 agent（缺省宿主自身，专属反思会话如 `tagent-session:tagent:reflection`）」，与事件驱动反思的单一主体一致

#### Scenario: 触发语义对齐事件驱动激活

- **WHEN** 审阅冥想叙事与实现文档
- **THEN** 触发描述引用真实 `external_input` 激活与 `meditation-autonomy` 的不变量，与事件驱动机制一致

### Requirement: 负反馈回顾(反思素材)

冥想回顾清单 MUST 包含 feedback 事件(任务失败 negative/用户不满)——失败教训是最高价值反思素材;分析提示应将 negative 归因到痛点。

#### Scenario: 负反馈进反思
- **WHEN** 冥想触发且回顾近期事件
- **THEN** 反思清单覆盖 feedback 事件(negative 优先归因)——负反馈→反思→结论的闭环闭合

### Requirement: 反思 digest 携带让位欠账

反思 digest SHALL 携带让位欠账：自上次冥想执行以来的 deferred 次数与最近一次让位时刻，渲染为 digest 中的一行——模型据此在结论中交代未兑现的反思及其成因；该计数为进程内会话语义（consumed 清零、重启归零），MUST NOT 引入新的持久化。

#### Scenario: 欠账可见

- **WHEN** 注入或消费时刻发生让位后，下一次冥想执行构建 digest
- **THEN** digest 含「让位 N 次」计数行，模型结论可引用该欠账交代覆盖范围

