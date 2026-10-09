# self-improvement-meditation Specification（delta）

## MODIFIED Requirements

### Requirement: 冥想定位叙事(自我改进引擎)

冥想 SHALL 叙述为**自我改进引擎的反思回合**：门控（interval/自身空闲/观察面 novelty）到点后，由**配置了冥想的 agent**（入口 agent 的缺省自察，或独立策展 agent 的跨域策展——同一机制、不同观察面与 session）的一个正常 turn 执行——读观察面事实链、产出三类产物（脚本/skill/prompt 改进）与经验卡片，产物纪律不变（prompt/skill/脚本类落盘后 SHALL 经 refine register 登记纳入评估保护）。novelty+idle 门控的"防自持"语义见 meditation-idle-gating。引擎×通道的文档叙事 SHALL 与单机制一致：不再有"in-loop/外部双形态"的通道表述——形态差异只是观察面配置与反思 session。

#### Scenario: 产物登记闭环对任何反思主体成立

- **WHEN** 任一反思回合写入了受控路径产物
- **THEN** refine register 义务照常适用，未登记产物进入 status 提醒

#### Scenario: 叙事与单机制一致

- **WHEN** 核对 wiki/README 的冥想叙述
- **THEN** 反思主体表述为"配置冥想的 agent（自察或策展）"，无双形态残留断言
