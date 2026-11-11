# self-improvement-meditation Specification（delta，MODIFIED 于既有能力）

> 头逐字匹配主 spec。本 delta 只改「冥想定位叙事(自我改进引擎)」：主语对齐 r14/r14b 的自察宿主与专属反思会话，触发语义改引 `meditation-autonomy`（状态机不变量）；**产物登记/refine 条款原文保留**（本轮不接线，属已登记规格债，不在本 delta 修改）。

## MODIFIED Requirements

### Requirement: 冥想定位叙事(自我改进引擎)

冥想 SHALL 叙述为**自我改进引擎的反思回合**：由状态激活（外部事件积累到规则阈值）后，由**配置冥想插件的 agent（缺省宿主自身，反思会话如 `tagent-session:tagent:reflection`）**的一个正常 turn 执行——读观察面事实链、产出改进产物（脚本/skill/prompt）与经验卡片；独立策展 agent 是显式声明的跨域选项（同一机制、不同观察面与反思会话）。状态激活、自管产出不计入状态积累与五不变量见 `meditation-autonomy`；本条款不再引用已退役的独立门控能力。引擎×通道的文档叙事 SHALL 与单机制一致：不再有"in-loop/外部双形态"的通道表述——形态差异只是观察面配置与反思会话。

#### Scenario: 叙事主语统一

- **WHEN** 核对 wiki/README 的冥想叙述
- **THEN** 反思主体表述为"配置冥想插件的 agent（缺省宿主自身，反思会话如 `tagent-session:tagent:reflection`）"，无双形态残留断言

#### Scenario: 触发语义不再指向独立门控

- **WHEN** 审阅冥想叙事与实现文档
- **THEN** 触发描述引用状态激活与 `meditation-autonomy` 的不变量，不引用已退役的 `meditation-idle-gating`
