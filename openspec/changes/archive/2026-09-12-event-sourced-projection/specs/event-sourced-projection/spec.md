## ADDED Requirements

### Requirement: 投影是事实链的纯回放（一等不变量）

投影（SessionProjection）SHALL 恒等于事实链（KV LSM 事件日志）的 fold/回放映射（**正常运行路径精确成立；退化恢复路径最终一致**，见下），兑现「投影是写入的旁路产物、事实链只在 KV 里」（memory-architecture.md:1060/1069）。运行期每次 `StoreEvent` SHALL 伴随 `projection.Add`（增量回放；`persistBusEvent` SHALL 对齐 stored-gate：StoreEvent 失败 SHALL NOT Append，交由 spill 恢复双写补回）；压缩折叠 SHALL 作为事实链的一条 compaction 事件（非游离于事实链外的投影态快照）。系统 SHALL NOT 把投影态持久化到事实链之外的独立 checkpoint（避免双真相源）。**文档化边界**：跨退化恢复的重启可能缺 spill 补写事件（spill 沿用旧 key，晚于 compaction 补写时被 tail 边界切掉）——最终一致非逐字节。

#### Scenario: 运行期投影随事实链增量回放
- **WHEN** 一条事件经 StoreEvent 落事实链
- **THEN** 投影 SHALL 同步 `Add` 对应 ref（同点投影），投影始终是事实链的派生映射

#### Scenario: 无游离 checkpoint
- **WHEN** 审查投影态的持久化位置
- **THEN** 投影重建所需状态 SHALL 全部来自事实链（原始事件 + compaction 事件），SHALL NOT 存在事实链之外的 KV-meta/独立快照作为第二真相源

### Requirement: 折叠向事实链追加 compaction 事件

压缩在**真折叠**（`buildRetainedRefs` 产出新综述 ref 时）SHALL `StoreEvent` 一条 `context_compress_summary` 正 key **compaction 事件**（事实链一等条目），载荷 SHALL 含：① composed 综述正文（逐字节）② 综述 ref 身份（`EventKey=-minTs` / **`EventType=context_compress`**（非载体类型 context_compress_summary）/ Timestamp / Role）③ **有序 retained-ref 列表**（保持投影交错顺序）：负 key `tool_chain` 合成 ref SHALL 存完整身份 + `EventSummary` 逐字节（store 无对应事件、不可 GetEvent），正 key ref 可只存 key（经 GetEvent 逐字节复原）④ fullBoundary。SHALL NOT 每轮超阈都写（仅真折叠时）。

#### Scenario: 真折叠追加 compaction 事件
- **WHEN** 一次压缩触发真折叠（产出新综述 ref）
- **THEN** SHALL StoreEvent 一条 `context_compress_summary` 正 key compaction 事件，载综述正文 + 综述 ref 身份（EventType=context_compress）+ retained-ref 列表（含 tool_chain 全身份+EventSummary）+ fullBoundary

#### Scenario: 未折叠不写
- **WHEN** 压缩轮 under-budget 或未产出新综述（投影未变）
- **THEN** SHALL NOT 写 compaction 事件（无常开写入）

#### Scenario: tool_chain 合成 ref 随 compaction 事件持久化
- **GIVEN** 折叠产物含负 key `tool_chain` 合成 ref（`- 工具链: …`，store 无对应事件）
- **WHEN** 写 compaction 事件
- **THEN** 其 retained-ref 列表 SHALL 携带该 tool_chain ref 的完整身份 + EventSummary 逐字节（SHALL NOT 只记正 key、SHALL NOT 把 tool_chain 拆成独立正 key 事件污染 recall）

### Requirement: compaction 事件滚动 supersede

折叠点 SHALL **先**查当前最新 compaction 事件 key（prior）→ 写新 compaction 事件 → `DeleteEvent(prior)`，使事实链中恒定只有最新一条活 compaction（`context_compress_summary` TTL 豁免，不 supersede 则无限累积）。**顺序 SHALL 为写前查 prior**：写后再查 timestamp_desc 会取到刚写的自己 → 自删 → 事实链断。compaction 事件 SHALL 带 `Metadata[compaction]=v1` 代际标记，supersede 查 prior 与 snapshot 选取 SHALL 限定带标记者——存量 legacy 固化物（无标记、TTL:-1 永活）SHALL NOT 被删或被选。compaction 事件自身 Timestamp SHALL 为写入时刻（非综述 minTs）。重启回放 SHALL 把最新 compaction key reseed 回压缩器状态，使重启后首次折叠能正确 supersede。

#### Scenario: 只留最新 compaction 事件
- **GIVEN** 已多次折叠、每次写过 compaction 事件
- **WHEN** 查询事实链中的 `context_compress_summary` 活事件
- **THEN** SHALL 只剩最新一条（旧的已被 DeleteEvent supersede）

#### Scenario: 不自删
- **WHEN** 折叠点执行 supersede
- **THEN** SHALL 先查 prior（写新之前）再 DeleteEvent(prior)，SHALL NOT 写后查导致删掉刚写的自己

#### Scenario: 不误删 legacy 存量固化物
- **GIVEN** 存量部署存在 legacy `context_compress_summary` 固化物（无代际标记、TTL:-1 永活）
- **WHEN** 首次折叠执行 supersede 与重建选取 snapshot
- **THEN** legacy 固化物 SHALL 不被 DeleteEvent、不被选为 snapshot（两查询均限定 `compaction=v1` 标记者），按主 spec 承诺 TTL 自然清退

### Requirement: 冷启动回放逐字节重建投影

系统 SHALL 提供 `RebuildProjectionFromWAL`，仅在启动期对**空投影**执行一次（投影非空则记 WARN 不静默跳过；SHALL 先于 spill 重放），按事件溯源「加载 snapshot + 回放尾部」重建：
1. 取最新 compaction 事件（snapshot）；
2. 按其 retained-ref 列表**交错顺序**复原——负 key 合成 ref（综述+tool_chain）用存的身份+EventSummary 逐字节（不 GetEvent），正 key ref 用 `GetEvent(key)`（缺失/墓碑降级跳过+WARN）→ `Replace` 进空投影（综述 ref 首位）；
3. **尾部重放**：经 `QueryOptions.MinEventKey`（**新增查询原语**，按 EventKey/写入序过滤，两 store 实现补齐）取 `EventKey > compaction 事件 key` 的事件，**分页取全**后**按 EventKey（写入序）升序**逐条 `projection.Add`——SHALL NOT 用 StartTime 近似（双时间分叉会错切 bus 滞留事件）、SHALL NOT 按 Timestamp 排序回放（运行期 append 序≡写入序，Timestamp 序会颠倒滞留事件）、SHALL NOT 静默截断（默认 Limit 截的是最新端）；
4. seed fullBoundary + 回种 meditationKeys（**snapshot 复原的与尾部回放的**冥想 agent_output 正 key ref 且 `Metadata[trigger_source]==meditation` 者调 `MarkMeditationKey`）。**前置**：`trigger_source` SHALL 经 Attribution 注入写入 agent_output 事件 Metadata（现仅活在内存 StateDelta、不落库——不补此前置则回种恒不触发）。

给定相同事实链与 config，重建后 `render(projection)` SHALL 与重启前逐字节一致（retained 事件 TTL 窗内，最紧 thinking_plan 3d），使 prefix-cache 命中。SHALL 用 `Replace` 进空投影，SHALL NOT 在活投影上 Replace、SHALL NOT 用纯 append（综述前置无法由尾追达成）。compaction 事件本身 SHALL NOT 作为独立正 key ref 进投影（仅用于重建负 key 综述 ref）。

#### Scenario: 逐字节重建（snapshot + 尾部）
- **WHEN** 进程 A 折叠若干轮（含 tool_chain 工具会话 + condenseCardLines 浓缩卡片）后继续对话产生尾部新事件，再重启为进程 B（空投影）
- **THEN** `RebuildProjectionFromWAL` SHALL 复原 [综述 ref + tool_chain + retained] 并回放尾部新事件，`render` 与 A 重启前逐字节相同（**测试装置须 `Content != EventSummary`** 以免掩盖 fullBoundary 漂移）

#### Scenario: 综述前置首位 + 交错顺序
- **WHEN** 重建装配投影
- **THEN** 综述 ref SHALL 在首位、tool_chain 与正 key ref SHALL 保持原交错顺序（Replace 切片序保证），SHALL NOT 错序

#### Scenario: 尾部不丢
- **GIVEN** 最新 compaction 事件之后事实链有 N 条新事件
- **WHEN** 重建
- **THEN** SHALL 分页取全并回放这 N 条尾部事件进投影（fail-before：只 Replace snapshot 不回放尾部 → 丢最近 N 条、非逐字节）

#### Scenario: bus 滞留事件顺序保真
- **GIVEN** 尾部含 task_settled 事件（入 bus 早、drain 写入晚于其后写入的 LLM 产出，Timestamp 序与写入序相反）
- **WHEN** 尾部重放
- **THEN** SHALL 按 EventKey（写入序）Add（与运行期 append 序一致），SHALL NOT 按 Timestamp 序颠倒（fail-before：Timestamp 序 → [E,F] vs 运行期 [F,E]，非逐字节）

#### Scenario: fullBoundary 与 meditationKeys 正确恢复
- **WHEN** 重建完成
- **THEN** fullBoundary SHALL 由 compaction 事件携带值 seed（或 anchorFullBoundary 重算）；折叠前标记的冥想键 SHALL 被 reseed（否则未折叠冥想 agent_output 再折叠时卡片 ★ 丢失）

#### Scenario: 无 compaction 事件时 no-op
- **GIVEN** 首次启动或从未触发真折叠（事实链无带 `compaction=v1` 标记的事件）
- **WHEN** 冷启动重建
- **THEN** SHALL 为 no-op（投影留空、维持现状行为），SHALL NOT 退化为全量回放（与运行期 fold 的等价性另议，不在本变更）

### Requirement: 折叠原文冷存不删、回放按 retained-ref 跳过

被折叠的原始事件 SHALL NOT 被删除（冷存），其 `[evt_key]` 召回票据 SHALL 仍可经 `GetEvent`/recall 回补原文。重建 SHALL 只复原 retained-ref 列表中的 ref + 尾部事件，折叠原文自然不进投影（被综述代表）。

#### Scenario: 折叠原文仍可 recall 回补
- **GIVEN** 一批事件已被折叠进综述
- **WHEN** 用其 `[evt_key]` 票据 recall / GetEvent
- **THEN** SHALL 仍能取回原文（折叠不删原文）

### Requirement: compaction 事件可召回且承载综述正文

compaction 事件 SHALL 保持 `context_compress_summary` 的 `Recallable:true`（用户裁决 2026-09-12），其可召回内容 SHALL 是〔历史综述〕正文（老历史的有用摘要），SHALL NOT 是内部元数据（retained-ref 列表/fullBoundary 等放 Metadata 供重建，不作召回正文）。

#### Scenario: 综述作为长期记忆可召回
- **WHEN** recall 查询命中老历史
- **THEN** compaction 事件 SHALL 可作为〔历史综述〕正文被召回（兑现「原文可忘、固化物长存」），召回内容为综述正文而非内部元数据

### Requirement: 移除游离快照补丁与 Replace-over-live

系统 SHALL NOT 保留 dev 的投影态补丁子系统（`persistSnapshotEvent` 每轮超阈写三态快照 + `ReplayProjectionHandler` 的 Replace 回灌活投影分支）。spill 恢复路径（`ReplaySpilled`）SHALL 保持 append-only（幂等），SHALL NOT Replace 活投影。

#### Scenario: spill 恢复不抹活投影
- **WHEN** memory degraded→normal 触发 `ReplaySpilled` 重放窗口事件，活投影已含更新条目
- **THEN** 重放 SHALL 仅 append（幂等去重），SHALL NOT Replace 掉更新的活条目

#### Scenario: 无游离快照事件
- **WHEN** 审查事实链中的压缩相关事件
- **THEN** SHALL 只有 `context_compress_summary` compaction 事件，SHALL NOT 有 dev 的三态快照 `context_compress` 正 key 事件
