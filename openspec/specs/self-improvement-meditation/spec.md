# self-improvement-meditation Specification

## Purpose

冥想即自我改进引擎:novelty+idle 门控的反思时机+三类产物生成(脚本/skill/prompt);产物纪律=落盘后必 refine register;adoption 核查经 refine status;文档叙事统一为引擎×通道。
## Requirements
### Requirement: 冥想产物纪律(登记闭环)
冥想 prompt SHALL 引导:改进产物(脚本/skill/prompt)落盘后立即调用 `refine register` 登记路径与 note;SHALL 明示「未登记的改进没有评估保护,也无法安全回滚」。adoption 核查 SHALL 引导使用 `refine status`(改进历史+窗口结论+未登记提醒)。

#### Scenario: 冥想产物流完整闭环
- **WHEN** 冥想触发且 LLM 按 prompt 产出改进(如更新 skill)
- **THEN** prompt 纪律引导其落盘后立即 refine register;下轮冥想的 adoption 核查经 refine status 获得该产物的窗口评估结论与采用情况

#### Scenario: 直改 prompt 文件为正确路径
- **WHEN** 冥想判定行为偏差根因在提示词
- **THEN** prompt 引导直接修改 resources/prompts/ 下对应文件(热重载即生效)+ refine register 登记——不再有「记录补丁建议留待后续」的搁置话术

### Requirement: 冥想定位叙事(自我改进引擎)

冥想 SHALL 叙述为**自我改进引擎的反思回合**：由状态激活（外部事件积累到规则阈值）后，由**配置冥想插件的 agent（缺省宿主自身，反思会话如 `tagent-session:tagent:reflection`）**的一个正常 turn 执行——读观察面事实链、产出改进产物（脚本/skill/prompt）与经验卡片；独立策展 agent 是显式声明的跨域选项（同一机制、不同观察面与反思会话）。状态激活、自管产出不计入状态积累与五不变量见 `meditation-autonomy`；本条款不再引用已退役的独立门控能力。引擎×通道的文档叙事 SHALL 与单机制一致：不再有"in-loop/外部双形态"的通道表述——形态差异只是观察面配置与反思会话。

#### Scenario: 叙事主语统一

- **WHEN** 核对 wiki/README 的冥想叙述
- **THEN** 反思主体表述为"配置冥想插件的 agent（缺省宿主自身，反思会话如 `tagent-session:tagent:reflection`）"，无双形态残留断言

#### Scenario: 触发语义不再指向独立门控

- **WHEN** 审阅冥想叙事与实现文档
- **THEN** 触发描述引用状态激活与 `meditation-autonomy` 的不变量，不引用已退役的 `meditation-idle-gating`

### Requirement: 负反馈回顾(反思素材)

冥想回顾清单 MUST 包含 feedback 事件(任务失败 negative/用户不满)——失败教训是最高价值反思素材;§2 分析提示应将 negative 归因到痛点。

#### Scenario: 负反馈进反思
- **WHEN** 冥想触发且回顾近期事件
- **THEN** 反思清单覆盖 feedback 事件(negative 优先归因)——负反馈→冥想→改进的闭环闭合

反思 digest SHALL 携带让位欠账：自上次冥想执行以来的 deferred 次数与最近一次让位时刻，渲染为 digest 中的一行——模型据此在卡片中交代未兑现的反思及其成因；该计数为进程内会话语义（consumed 清零、重启归零），MUST NOT 引入新的持久化。

#### Scenario: 欠账可见

- **WHEN** 注入或消费时刻发生让位后，下一次冥想执行构建 digest
- **THEN** digest 含「让位 N 次」计数行，模型卡片可引用该欠账交代覆盖范围

