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
文档(README/wiki/meditation.md)SHALL 统一将冥想定位为「自我改进引擎(反思时机+产物生成)」,并与 refine(git 登记通道)/consolidation(记忆通道)互引;MUST NOT 再使用「回顾沉淀 ★ 卡片进长期记忆」的旧定位描述。

#### Scenario: README 叙事一致
- **WHEN** 读者查看 README 特性表与 wiki platform/tool 篇
- **THEN** 冥想行描述为自我改进引擎,refine 行描述为 git 原生改进通道,两者关系(引擎×通道)在文案中可见

### Requirement: 负反馈回顾(反思素材)

冥想回顾清单 MUST 包含 feedback 事件(任务失败 negative/用户不满)——失败教训是最高价值反思素材;§2 分析提示应将 negative 归因到痛点。

#### Scenario: 负反馈进反思
- **WHEN** 冥想触发且回顾近期事件
- **THEN** 反思清单覆盖 feedback 事件(negative 优先归因)——负反馈→冥想→改进的闭环闭合
