# event-sourced-projection Specification

## Purpose

投影作为事实链（KV LSM 事件日志）的纯回放映射：压缩折叠以一等 compaction 事件（`context_compress_summary`，代际标记滚动 supersede）入事实链，冷启动经「最新 compaction snapshot + 尾部重放」逐字节重建投影（prefix-cache 复用）；单一真相源（事实链），无游离 checkpoint。覆盖 R1（上下文跨重启连续）。
## Requirements
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

系统 SHALL 提供 RebuildProjectionFromWAL，启动期对空投影执行且先于 inbox/mem_spill 重放。非空投影 SHALL 不被 Replace，并记录 skipped 原因。存在有效 compaction 时 SHALL 按 retained-ref 交错顺序复原：负 key 合成 ref 使用载荷全文，正 key 从唯一事实链水合；尾部 SHALL 经 MinEventKey 分页读取并按 EventKey 写入序追加，不使用 Timestamp/StartTime 近似。fullBoundary SHALL 使用载荷值，snapshot 与尾部冥想键均须回种。compaction 自身 SHALL NOT 独立进入投影。

相同事实链、配置与 TTL 存活前提下，正常 snapshot 路径的 render SHALL 字节一致。缺键或读取失败 SHALL 形成结构化 partial/failed 结果，不假称逐字节。无 compaction 时 SHALL 进入有界 fallback，不再 no-op；空库必须经成功扫描确认。

#### Scenario: 逐字节重建（snapshot + 尾部）
- **WHEN** 使用 Content 不等于 EventSummary 的数据，折叠后继续写尾部并独立进程重启
- **THEN** retained refs、合成 tool_chain、边界和尾部恢复后渲染字节一致

#### Scenario: 综述前置首位 + 交错顺序
- **WHEN** 恢复交错 retained refs
- **THEN** 综述在首位，合成与正 key refs 保持载荷次序

#### Scenario: 尾部不丢
- **WHEN** snapshot 后存在跨多页尾部
- **THEN** 所有可见尾部均按 EventKey 追加，不静默截最新端

#### Scenario: bus 滞留事件顺序保真
- **WHEN** Timestamp 与写入序相反
- **THEN** 恢复遵循 EventKey 写入序而非 Timestamp

#### Scenario: fullBoundary 与 meditationKeys 正确恢复
- **WHEN** snapshot 与尾部都含冥想产出
- **THEN** boundary 使用载荷值，双方冥想 keys 均回种

#### Scenario: 无 compaction 事件时恢复
- **WHEN** 事实链有原始事件但从未折叠
- **THEN** 执行 fallback，非空链不被默认为空会话

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

### Requirement: 无锚恢复不静默截断

无锚回放 MUST 分页扫描并先过滤非投影事件，再保留最新 500 个有效事件，按 EventKey 渲染；中间内存 SHALL 有界。超护栏 MUST 返回 status=partial 和准确 truncated_events，日志、diagnostics、首次模型请求均可辨。被过滤的 task/receipt/快照记录 SHALL NOT 占用 500 的投影名额。

#### Scenario: 超护栏长链冷启动
- **WHEN** 无 anchor 且有 600 条有效事件及 600 条非投影记录
- **THEN** 保留最新 500 条有效事件，truncated_events=100，结果 partial，内部记录不挤掉有效上下文

### Requirement: 恢复观测覆盖全误差面

重建 MUST 返回并保存 RecoveryResult，包含 mode/status、扫描/投影/截断数、missing_keys、pages_failed、batch_errors、payload_errors、耗时。水合 MUST 按请求 key 与返回 key 对账；底层返回空集合与 error 不得被空库分支吞掉。status=full 仅在所有相关扫描、载荷解析、水合均完整且未截断时成立。partial/failed SHALL 进入 diagnostics，并在首次模型请求尾部注入一次简短运行态提示，不成为历史第二真源。

#### Scenario: tail 分页部分失败
- **WHEN** tail 某页查询失败
- **THEN** pages_failed≥1，status=partial/failed，diagnostics 与模型可见提示一致

#### Scenario: 批量读静默漏键
- **WHEN** GetEvents 未返回 error 但返回键集合少于请求集合
- **THEN** missing_keys 列出差集，不能报告 full

#### Scenario: 空结果伴随错误
- **WHEN** 首次扫描即错误且结果为空
- **THEN** status=failed，不记录“空事实链正常恢复”

#### Scenario: payload 无法解析
- **WHEN** compaction 存在而 payload 无法解析
- **THEN** payload_errors≥1，结果 failed，保留原始数据且对宿主可见

