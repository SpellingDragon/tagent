## ⚠️ 归档裁决（2026-09-12 fresh-eyes 三路复验：设计不成立，归档为设计决策记录）

本变更**未实现**（tasks 0/25）。经三路独立 fresh-eyes CodeReview 对照 dev 真实代码，**三个子系统的承重假设全部被推翻**，故不照此实现；归档留存分析与修正方向（LEDGER 重编原则 #1：裁决集中记录、不因归档丢失）。下方原设计正文保留作「原提案」记录，以本段为准。

**元教训**：核心原则「运行态 = 基底的可重算视图」**有边界——path-dependent 的派生态不可重算、必须持久化**。本设计凭架构美感推断、未核实代码现状（与本会话早前误判「dev 已修好」同类错误）。

### R1 压缩重建：recompute-from-WAL 做不到逐字节（3 🔴）
- 综述 ref 前置首位（context_compressor.go:1117），`Append` 只能尾追 → 升序重放把它落中段 → 前缀第一条即断。
- 折叠集非连续（L3 连续前缀 ∪ L1/L2 零散工具 key）且折叠事件**不墓碑**（agent/ grep tombstone 零命中）→ 单个「折叠区间」表达不了、dev 也无此结构 → 重建无法精确切出存活集。
- fullBoundary 只在 over-budget 分支赋值（:332），under-budget 稳态恒 0 → 旧 ref 从摘要渲染漂成全文水合。
- dev 的 `ByteIdenticalAssembly` 走的是被删的 Replace 快照路径、且装置设 `Content==EventSummary` 掩盖漂移 → **证明不了 recompute**（我上轮误引为论据）。
- **修正方向**：压缩视图 path-dependent、非 WAL 纯函数 → **dev「持久化派生态」直觉对**，真 bug 在触发点（spill 恢复而非冷启动）、载体（可召回正 key context_compress）、Replace-over-live。协调解 = 保留精确持久态（retained 集 + fullBoundary + 综述 ref 身份），**冷启动进空投影 Replace（安全）**、非召回载体、threshold 从 config、活投影 spill 路径才 append-only。**task 1.4「删快照」错，应「改造快照」。**

### R2/R3 任务接续：奉为范本的 resident_recovery 生产是死代码（4 🔴）
- named 会话 `n-*` 被 `ListSessions` 的 `tagent` prefix 过滤（`WithTmuxPrefix` 生产从不调用）→ `ReattachResidentSessions` 枚举**恒空**、无端到端测试。
- task 板**零持久化**（task_board.go:21「never persisted」）、`TaskSpec` 的 Relaunch/ResumeFn/Alive 是**闭包不可序列化**、`ResidentMeta` 缺 command/workdir/env/**origin**/task_id → 「从持久 task 元数据重建板」**无数据源**；Origin 不持久 → 异步结果回不到原会话。
- fail-safe 三态只改任务层探针，真屠杀路径在 monitor 层 `IsPaneDead`/`ProcessExists`（err→assume dead）未堵。
- 三态在 `has-session` 上不可实现（dead 与 unknown 同 exit 1，实测 tmux 3.6a）→ 须改 `list-sessions` 名单正控。
- **修正方向**：先插入「基底修复」前置阶段（修枚举 prefix、task 板持久化含 Origin、闭包可重建、monitor 三态贯穿、收养续期、oneshot 长命令 orphan 豁免），再谈折叠对账。

### R4 配置热更新：泛化 maybeSyncLocked 是范畴错误（2 🔴）
- `maybeSyncLocked` 只改调用时查的**间接 map**（「从不改 agent 工具声明集」registry.go:38）；tools/subagent 是**结构**（WithTools 烘进 option.Tools 无 mutator、fwAgent 未持有、子 agent 是 AgentToolWrapper 非框架 SubAgent）→ **无热换缝**。dev 自己归为「snapshot rebuild incremental B」、结构变更打「RESTART required」。
- tools 增删被我错分为 R4a 低风险热应用，实为结构变更需重启。
- **修正方向**：R4 收缩到只热更新间接/原子层（mcp/prompt/tool-desc/threshold/evolution，大半已热）；tools/subagent 结构热换 scope 出去单独立项；**别删 org_coordinator.go**（阶段四唯一重建脚手架）。

### 独立于本设计、仍成立的真 dev bug（值得单独立项修）
探测失败屠杀看板（monitor 层 fail-dead）、`SettleFailed` 渲染成「✓ completed」、resident_recovery 枚举死代码、task 板不持久化、快照 recall 污染、Replace-over-live、fullBoundary race。这些不依赖本变更被推翻的大原则，是确定缺陷。

### delta specs 处置
4 个 delta（projection-restart-rebuild / resident-config-hot-sync 新；task-skeleton-compression / task-registry-and-board 改）编码的是被推翻的需求 → **归档 --skip-specs，不并入主 specs**（task-registry delta 还与主 spec「registry 纯内存不持久化」矛盾；task-skeleton「narrative-only」已被 R1 裁决推翻）。

---

## Context

tagent 的常驻部署（wechat-bot）需要 bot 成为「能自我热换、跨重启连续」的常驻体。dev 分支为此实现了三个特性，但各自**偏离了 main 已确立的两个协调范式**：

- **范式一 — 重启期从基底重算视图**：`tool/action/resident_recovery.go`（24399ad）。tmux 会话活在 tmux server（跨重启存活），agent 内存追踪不存活。解法：spawn 时把**不可重算的参数**持久化为 `ResidentMeta`（`sess-<id>.json`）；启动时 `ReattachResidentSessions` 对账 **tmux list（存活性真相）∩ metadata（参数真相）→ 重建追踪**。
- **范式二 — 配置变更期 diff-apply**：`tool/mcp/registry.go:maybeSyncLocked` + `prompt.Source`。config 文件是基底，惰性查 mtime → 重解析 → **只 diff-apply 目标段** → fail-closed 保旧值。

dev 的三处分歧：② 压缩事件溯源把**整个压缩视图**快照成平行真相源（正 key `context_compress` 事件 + Replace 回灌）；③ task 对账挂在 `List()` **查询热路径**且探测失败即判死；④ org 热重载**又造第三套** mtime 机制（含死代码 `org_coordinator.go`）。同时 main 自身对 R1（对话上下文跨重启）**完全缺失重建原语**——投影重启即空，narrative（LLM 增量综述）负 key 仅投影、不进 WAL、不可重算。

约束：不改压缩的确定性无状态视图变换本质；不破坏「投影是装配唯一源」；recall 两段式票据哲学；prefix-cache 复用要求重启后 render 逐字节一致。

## Goals / Non-Goals

**Goals:**
- 把 main 自有两范式统一推广到全部常驻运行态：**运行态 = 持久基底的可重算物化视图；只持久化不可重算块；在触发点重算；绝不快照整个视图、绝不在查询热路径对账。**
- R1：完备 WAL（持久化 narrative）+ 启动期投影重建 → 逐字节上下文复原 → prefix-cache 复用。
- R2/R3：task 板对账折叠进启动 Reattach + fail-safe 探测；`List()` 只读。
- R4：单一 config-watcher 泛化 MCP registry 范式到 tools/subagent/threshold 全段热更新。
- 消除 dev ②③④ 引入的 5 个 🔴 + 4 个 🟠。

**Non-Goals:**
- 不改压缩折叠算法本身（卡片行/段龄定级/确定性重折叠保持）。
- 不做跨机器重启的 task 接续（ResidentMeta 在 tmp，仅覆盖进程热换；机器重启的 tmux 本就消失）。
- R4 不追求「任意段皆可热换」——明确区分可热应用 vs 需重启段。
- 不在本变更内重写 recall（narrative 可召回性是既有 `context_compress_summary` 类型属性，本变更只恢复其产生源）。

## Decisions

### D0：统一原则 — 视图 = 基底的可重算物化

| 需求 | 视图（内存） | 持久基底（真相） | 重算触发 | 只持久化的不可重算块 |
|---|---|---|---|---|
| R1 对话连续 | projection | WAL 事件 + **narrative** | 启动 `RebuildProjectionFromWAL` | LLM 滚动综述 |
| R2/R3 任务连续 | task 板 + tmux 追踪 | tmux server + ResidentMeta | 启动 `ReattachResidentSessions` | 会话参数 |
| R4 配置热更新 | agent 组合 | tagent.yaml | mtime → diff-apply | —（配置即基底）|

**理由**：main 已在 R2/R3（resident_recovery）与 R4 片段（MCP registry）各自实现此原则；R1 缺失、dev ②③④ 违背。统一后三者同一心智模型。**备选否决**：dev 的「快照整个视图」= 平行真相源（引发 Replace 冲突/race/recall 污染），违背原则。

### D1：narrative 持久化 = `context_compress_summary` WAL 事件（非 checkpoint 边车）

**选择**：L3 折叠点 `synthesizeRollingNarrative` 产出新综述时，`StoreEvent` 一条正 key `context_compress_summary` 事件（TTL 豁免、Recallable+Embeddable），载 narrative 逐字节 + 折叠 key 区间；滚动 supersede（墓碑上一条）。

**理由**：①与 `ResidentMeta` 同构（都只持久化不可重算块进基底）；②单一真相源（WAL），投影是其纯折叠；③兑现 memory-architecture「原文可忘、固化物长存」原则（原文 TTL 墓碑后 narrative 是唯一存活合成记录）；④契合用户「通过 WAL 重建」诉求。**备选否决**：投影 checkpoint 边车（保留 KV 键、非事件）——虽 blast radius 更小、不动 recall，但是 WAL 之外的第二 artifact、不兑现固化物长存、非「WAL 重建」。

**按减法判定标准校验**（`context-efficiency-and-trajectory` design.md:22 的方法论）：删掉 narrative 持久化后行为信息**变少**（重启无法逐字节复现 + 原文墓碑后综述不可恢复）→「缺失，补最小量」。**只补 narrative**（卡片行等可重算产物仍不落库、走 `[evt_key]` 票据）——不重蹈 legacy 固化物「冗余副本」覆辙。

### D2：R1 做成 resident_recovery 的显式同构镜像

**选择**：新增 `RebuildProjectionFromWAL`，命名/结构/时序对齐 `ReattachResidentSessions`：启动期一次性、WAL（存活性/内容真相）→ 逐条 **Append**（幂等）→ 首个 BeforeModel 确定性重折叠。**只用 Append，绝不 Replace 活投影。**

**理由**：两个重启恢复走同一心智模型，降低认知与维护成本；Append-only 天然消除 dev 🔴#1（Replace 抹活投影）与 🔴#2（跨 goroutine 写 fullBoundary——重算在 BeforeModel 单 goroutine）。**备选否决**：dev 的 `ReplayProjectionHandler` Replace 分支（接到 spill 恢复、破坏性）。

### D3：R4 分阶段 — 统一 watcher + threshold/tools 先行，subagent 后置

**选择**：阶段一泛化 `maybeSyncLocked` 为唯一 config-watcher，接入已热段（mcp/prompt/tool-desc/evolution）+ 新增 compress_threshold、tools 增删；阶段二再做 subagent 热重组（硬骨头：in-flight 引用、turn 边界换点）。

**理由**：threshold/tools 热应用低风险（原子换值 / 增删 leaf 工具）；subagent 重组涉及运行中 agent 树重构 + in-flight 子 agent 调用，需 turn 边界安全换点，单独一阶段控风险。**备选否决**：dev 的 org 并行 mtime（第三套机制 + 死 coordinator）。

### D4：R2 task 板重建并入 ReattachResidentSessions

**选择**：启动期 `ReattachResidentSessions` 重建 tmux 追踪后，**连带**从「重发现的会话 + 持久 task 元数据」重建 TaskManager 板；运行期存活性对账降为**可选限速后台 sweep**，`List()` 回归只读。探测三态：`tmux list-sessions` = 存活真相，命令失败 = **unknown ≠ dead**（fail-safe 保留）。

**理由**：一处启动恢复任务全态，避免 dev ③ 的第二套热路径对账；fail-safe 探测消除「tmux 短暂不可达 → 全看板屠杀」🔴。**备选否决**：dev 的 `List()` 热路径 reconcile（查询变有副作用 + 热路径 shell out + 探测失败判死）。

### D5：threshold 只从 config 取，不持久化

**选择**：threshold 是配置权威（`config.go` CompressThreshold + R4 热更新），**不进 narrative 事件、不重启回灌**。

**理由**：消除 dev 🟡#11（快照回灌陈旧 threshold 覆盖运维热改）；配置派生态不该被会话状态快照。**备选否决**：dev 快照存 threshold + `UpdateThreshold` 回灌。

## Risks / Trade-offs

- **[recall 面变化：narrative 进 recall/语义索引]** → 它是长期策展记忆，本就该可召回（兑现固化物长存）；严格 scope 到 narrative-only（卡片行仍票据），滚动 supersede + 墓碑控增长；embedding 成本每折叠一次（低频）。
- **[启动重建成本 O(活事件)]** → 被 TTL/容量遗忘有界；一次性启动开销；增量水位（只重放上次 checkpoint 之后）留作后续优化，不入本变更。
- **[逐字节一致依赖重算确定性]** → dev 自己的 `TestCompressReplay_ByteIdenticalAssembly` 已证「重放+重折叠逐字节一致」；卡片行确定性、narrative 持久化逐字节复现（绝不重启时重新生成）→ 前缀稳定。
- **[subagent 热换 in-flight 引用]** → turn 边界换点；阶段二单独处理（D3）；未就绪前 subagent 变更归「需重启」段。
- **[与 dev 活跃 dogfood 冲突]** → 本变更 supersede dev ②③④；实施时机与 dev 协调（见 Migration）。
- **[context_compress_summary 曾被移除，恢复需防重蹈冗余]** → 减法标准校验（D1）+ 严格 narrative-only scope + 卡片行不落库守卫测试。

## Migration Plan

1. **阶段 1（R1）**：`context_compressor.go` 折叠点落 `context_compress_summary` 事件（+ supersede 墓碑）；新增 `RebuildProjectionFromWAL` + 启动接线；删 dev 快照子系统（`persistSnapshotEvent`/`snapshot.go`/Replace 分支）。回归：narrative 持久化 + 重启逐字节重建 + fail-before（去持久化则前缀漂移）。
2. **阶段 2（R2/R3）**：`ReattachResidentSessions` 连带重建 task 板；`List()` 只读化；探测三态 fail-safe；`SettleFailed` event_bus 档 + retire 静默 detector + applyStatus 终态守卫。回归：探测失败不杀活会话（fail-before）+ SettleFailed 渲染 failed。
3. **阶段 3（R4a）**：泛化 `maybeSyncLocked` 为统一 config-watcher；接入 threshold/tools；删 `org_coordinator.go` 死代码 + `thresholdPct` 旁路字段。回归：mtime 改 threshold/tools 免重启生效 + 结构变更 fail-closed。
4. **阶段 4（R4b）**：subagent 热重组（turn 边界安全换点 + in-flight 引用处理）。
- **回滚**：各阶段独立可 revert；R1 narrative 持久化即使开启，未重启则零行为变化（重建只在启动触发）；R4 watcher 未配 mtime 绑定则等同现状。
- **分支协调**：优先在 dev 活跃 dogfood 收敛后 refactor ②③④ 到本设计；或本变更在 main 侧实现、dev 合并时以本设计为准 supersede。

## Open Questions

1. `context_compress_summary` 恢复产生后，narrative 是否**同时**进语义索引（Embeddable）供 recall，还是仅重启重建用（可加 `Recallable:false` 变体）？倾向：保持可召回（兑现固化物长存），但需评估 recall 结果里综述 vs 票据的呈现。
2. R1 启动重建：全量重放起步，增量水位（checkpoint + 只重放增量）何时值得引入？取决于活事件规模实测。
3. R4 subagent 热重组的 in-flight 语义：正在执行的子 agent 工具调用遇到其定义被热删/热改，如何收敛（drain / 保留旧定义至 turn 结束）？
4. dev ②③④ 的收敛方式：refactor-in-place on dev，还是 main 侧实现后 dev 合并 supersede？取决于 dev dogfood 节奏。

## 场景模拟暴露的缺口（2026-09-12 走查「热换装 + 超长任务 + 磁盘曾闪断 + 运行中改配置」）

5. **narrative 折叠区间语义须钉死**：重建时 WAL 里同时有 narrative 综述事件与其后未折叠的正 key 事件。综述事件载的「折叠 key 区间」必须精确表达「哪些 key 已被综述吸收」，重建据此只渲染 [综述 ref] + [区间后的事件 refs]，SHALL NOT 重复渲染被吸收的事件。区间边界语义（含/不含端点、与 recent_full_count 窗口的交界）须在 1.1/1.2 实现时定死并有回归。
6. **重建后重折叠的幂等性**：重建把 narrative 综述事件复原为负 key 综述 ref 后，首个 BeforeModel 的确定性 `Compress` SHALL 识别该 ref 为「已折叠综述」并吸收/保留它，SHALL NOT 把它当普通事件重新折叠（否则综述被二次折叠 → 前缀漂移）。dev 的 `ByteIdenticalAssembly` 覆盖的是「含快照 Replace」路径，纯 append 重建 + 重折叠的逐字节一致性须由 1.5 独立回归证明。
7. **转世 meta 信号的注入位置影响前缀缓存**：R1 重建让 [system]+render(投影) 逐字节复原（前缀缓存主体命中）。但转世 meta 信号（PID/时间/断点，每次重启不同）若注入在重建投影**之前**，会成为前缀一部分 → 破坏其后所有缓存。故 meta 信号 SHALL 注入在**消息尾部**（与任务板同区，缓存损失限于自身），SHALL NOT 插在重建投影与 system 之间。这要求 ① 转世通报收窄为 meta 信号后，注入位置从「投影内/前」改到「尾部」。
