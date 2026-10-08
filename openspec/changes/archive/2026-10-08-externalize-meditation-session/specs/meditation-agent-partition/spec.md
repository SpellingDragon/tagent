# meditation-agent-partition Specification

## Purpose

冥想可作为独立配置的同构 agent 运行：经授权读被观察分区，以跨分区谱系判据决定何时反思，锚点与空闲门沿用既有语义。封死跨 agent 永动回路：任何自管谱系产出（冥想、巩固建议）不构成任何观察者的新鲜度。

## ADDED Requirements

### Requirement: 冥想 agent 经配置声明并以授权分区为观察面

冥想 agent SHALL 是 `agents:` 下的普通 agent 定义（无新 agent 类型/运行时机制），其观察面 SHALL 取自授权读取的分区集合（`meditation.observed_namespaces`，缺省回落 `memory.read_namespaces`）；装配期 SHALL 校验 `observed_namespaces ⊆ read_namespaces`，越界具名拒绝启动（纸面约束不够：不强制则未授权分区可静默进入判据）。未授权分区的事件 SHALL NOT 进入其 novelty 判据。观察面变更 SHALL 走结构换代（构造期读取，不入数值热参、不静默生效）。

#### Scenario: 未授权分区不构成新鲜度

- **WHEN** 观察面外的分区在 last-meditation 之后写入了用户事件
- **THEN** 冥想 agent 的 novelty 门保持关闭，不触发

#### Scenario: 观察面扩字段走换代

- **WHEN** 修改 `meditation.observed_namespaces` 并热保存
- **THEN** 结构指纹变化触代替换生效（meditation 块本在指纹参与集内，扩字段随全字段序列化自动覆盖）；若配置同时含不可热字段则整批具名拒绝，不静默半应用

### Requirement: novelty 判据为跨分区非自管谱系新事件

novelty SHALL 定义为：被观察分区内存在 `Timestamp > lastMeditation` 且谱系**非自管**的事件。自管判定 SHALL 调用 event 包 lineage 单源派生（`SelfManagedLineage`），MUST NOT 在消费方复刻清单。数据面 SHALL 使用 `memory.QueryEvents` 读后过滤 `Metadata[trigger_source]`，MUST NOT 建第二索引。`trigger_source` 缺失的存量事件 SHALL 按"未知谱系"处理且**不计入** novelty（保守取向）。

#### Scenario: 跨分区用户事件触发

- **WHEN** 被观察分区有新用户事件（trigger_source=user）晚于上次冥想
- **THEN** novelty 门打开，配合空闲门满足后触发冥想

#### Scenario: 防永动（自管产出不计入）

- **WHEN** 冥想 agent A 或另一冥想 agent B 在共享分区写入冥想产出（自管谱系），且无任何非自管新事件
- **THEN** 无论经过多少个空闲窗口，novelty 门保持关闭——跨 agent 反思链路在结构上不可能自持

#### Scenario: 存量事件无谱系键

- **WHEN** 一条 last-meditation 之后的事件不含 `trigger_source` 元数据
- **THEN** 判为未知谱系、不计入 novelty；判据行为可从日志观测

### Requirement: 门控与锚点沿用既有语义

空闲门（距最近回合结束 ≥ min_gap）、触发节奏（interval）、三锚点持久化（AnchorStore，重启不误触发）SHALL 沿用 MeditationManager 既有契约不变；本能力只替换 novelty 的取数面（同 agent 输入侧锚 → 跨分区谱系判据）。

#### Scenario: 锚点跨重启

- **WHEN** 冥想 agent 重启且锚点文件在位
- **THEN** 恢复三锚点，不立即误触发；novelty 判据窗口连续

### Requirement: 外部判据水位与锚点复用

外部观察形态的 novelty 判据 SHALL 以 `lastMeditation` 锚为时间水位（存在 `Timestamp > lastMeditation` 的非自管事件即 novelty）；`lastUserInput` 锚 SHALL 按注入规则继续更新但不参与外部判据，其保留理由（回切 in-loop 形态时输入侧语义连续）SHALL 以代码注释固化。锚点持久化 SHALL 沿用 AnchorStore 三锚结构不变（不新增锚字段）。

#### Scenario: 水位判定

- **WHEN** 被观察分区存在 Timestamp 晚于 lastMeditation 的非自管事件
- **THEN** novelty 门打开（水位锚推进发生在有效冥想触发时，沿用既有自锁语义）

#### Scenario: 回切 in-loop 后语义连续

- **WHEN** 移除观察面配置并重启，期间曾有用户注入
- **THEN** 判据回切输入侧锚，lastUserInput 已按注入规则持续更新，无需任何迁移

### Requirement: 冥想 agent 的产出为普通事件

外部化冥想的产出（经验卡片/综述）SHALL 以普通事件写入**自身分区**的事实链；MUST NOT 写 compaction 事件（单压缩权不可转移）、MUST NOT 直接修改任何被观察分区的状态。对目标上下文的影响 SHALL 只经"目标自然折叠吸收共享事实链中的新事件"间接发生。

#### Scenario: 产出落自身分区

- **WHEN** 冥想 agent 完成一轮跨域巩固产出卡片
- **THEN** 卡片事件在冥想 agent 自己的分区；目标分区零写入
