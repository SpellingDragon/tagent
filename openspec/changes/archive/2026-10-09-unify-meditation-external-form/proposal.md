# Proposal: unify-meditation-external-form

## Why

上一变更（externalize-meditation-session）留下双形态：两套 novelty 判据（in-loop 注入侧 user 锚 / 外部跨分区谱系水位）、三锚结构、双 digest、双叙事——一个能力两套机制是长期税。用户裁决统一，且给出统一模型：**到期触发＝向某个 session 注入一个特定的冥想事件；"in-loop 还是外部"只是 session id 与观察面配置的差异，冥想管理器自行识别**。由此 in-loop 不删除，而是收敛为"观察面＝自身分区"的特例：判据、动作、锚、digest 全部单套。

## What Changes

1. **单机制**：MeditationManager 的动作恒为"向**本 agent 循环的 session** 注入冥想输入事件"。入口 agent 配冥想＝反思进业务 session（共享会话上下文，自体维护语义保留）；独立策展 agent 配冥想＝反思进策展 session。无需跨 agent 注入，投递缝（DeliverToAgent）仍只服务卡片回流。
2. **novelty 单判据**：观察面内存在水位（lastMeditation）后的**非自管谱系**事件。观察面缺省＝**[自身分区]**（零 breaking：现有部署不改配置即等效原 in-loop 触发——用户消息本就是自身分区的 user 谱系非自管事件）；显式 `observed_namespaces` 可含自身与他人（混合策展：既回顾自己也看别人）。`lastUserInput` 锚、注入侧挂臂、双分支判据整体删除。
3. **锚点两字段**：lastTurnEnd（自身空闲，任意谱系回合含投递/失败都算忙）+ lastMeditation（水位，触发即自锁）。旧三锚文件多余键忽略，无迁移。
4. **digest 单覆盖面**：观察面概况为主（分谱系计数+带事件键最近活动——观察自身时即"自体近况"）；自身任务板为可选段。
5. **session 安排（本次一并落档）**：注入目标恒为本 agent 的 StartLoop session；命名空间约定——业务线沿用宿主路由（wechat-bot 现行 `wechat-session`）、反思线用保留名（`curation`），永不与用户路由撞名。wechat-bot 示例：入口 meditation 块保留（缺省自察），新增启用的 curator agent（observed=业务分区、deliver_to=[entry]、`curation` session）。
6. **文档/规格**：五个主 spec 条款收敛为单机制叙事（REMOVED×2 + MODIFIED×6）；wiki §2.14 重写；README 双语同步。

## Capabilities

| Capability | 变化 |
|---|---|
| meditation-agent-partition | 观察面缺省=[自身]；显式可含自身与他人（⊆ read_namespaces，自身恒合法）；水位条款替代"外部判据"条款 |
| meditation-idle-gating | novelty 唯一判据=观察面谱系水位；输入侧锚条款 REMOVED |
| meditation-self-state-digest | 覆盖面统一（观察面概况为主、任务板可选） |
| self-improvement-meditation | 反思主体=任何配置冥想的 agent（入口自察或策展人）；产物纪律不变 |
| cross-session-delivery / consolidation-triggers | 不动 |

## 边界与依赖

- **零 breaking**：不配 observed 的现有 meditation 用户行为不变（判据从"注入侧 user 锚"换为"自身分区 user 谱系事件"，触发时机语义等价——用户输入入库即非自管事实）。
- **不动**：阈值折叠链、投递缝四道拒绝、lineage 单源、自动压缩的单压缩权。
- **不做**：跨 agent 注入冥想事件（动作恒为本 agent session，DeliverToAgent 只回流卡片）；多策展网格；隐式自动装配策展人。
