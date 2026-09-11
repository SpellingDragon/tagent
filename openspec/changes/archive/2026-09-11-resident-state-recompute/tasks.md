> **⚠️ 归档裁决（2026-09-12）**：以下 25 项**未实现且不应照此实现**——fresh-eyes 复验推翻了设计承重假设（见 design.md 顶部「归档裁决」）。尤其 task 1.4「删快照子系统」已被证错（应「改造快照」：冷启动进空投影 Replace 安全、非召回载体）；2.x 依赖的 resident_recovery 是生产死代码、task 板无持久化数据源；3.x tools 热应用不可行。保留作为「原计划」记录。

## 1. 阶段一 — R1 对话上下文跨重启连续（WAL 完备 + 投影重建）

- [ ] 1.1 `agent/compress/context_compressor.go`：`synthesizeRollingNarrative` 折叠点落一条正 key `context_compress_summary` 事件（载 narrative 逐字节 + 折叠 key 区间，区间语义精确到「哪些 key 已被综述吸收」含端点与 recent_full_count 窗口交界），滚动 supersede（墓碑上一条综述事件）；无 `summary_model` 时不产生；卡片行等可重算产物不落库
- [ ] 1.2 新增 `RebuildProjectionFromWAL`（与 `ReattachResidentSessions` 结构同构）：`QueryEvents(未墓碑事件, key 升序)` → 逐条经幂等 `AppendProjectionRef`（综述事件→负 key 综述 ref，普通事件→各自 ref）；**只 Append 不 Replace**；重建后首个 BeforeModel 的 Compress 须识别综述 ref 为已折叠并吸收、不二次折叠（幂等，防前缀漂移）
- [ ] 1.3 `build_agent.go`/`tagent.go` 启动接线：store 就绪后、事件循环 go statement 前调 `RebuildProjectionFromWAL`（仅对空投影一次，happens-before 保证）；threshold 从 config 取，不回灌
- [ ] 1.4 删 dev 快照子系统：`persistSnapshotEvent`（context_manager.go）、`agent/compress/snapshot.go`、`RestoreCompressionSnapshot`、`ReplayProjectionHandler` 的 Replace 快照分支（**保留** Append + 冥想 Mark 分支）；spill 恢复路径保持 append-only
- [ ] 1.5 回归（fail-before/pass-after）：①有 summary_model 落综述事件、无则不落 ②冷启动逐字节重建（进程 A 压缩落 WAL → 进程 B 重建 → `render(projection)` 逐字节等，含综述文本）③fail-before：去 narrative 持久化则 B 重建后综述缺失/前缀漂移 ④spill 恢复 append-only 不抹活投影（构造活投影有更新条目 + 重放旧事件 → 不 Replace）⑤重建后重折叠幂等（综述 ref 不被二次折叠、render 逐字节一致，独立于 dev 含 Replace 路径）⑥折叠区间正确（被综述吸收的事件不重复渲染）
- [ ] 1.6 ① 转世通报收窄为 meta 信号（删 D8 WAL 尾现场块——R1 重建已恢复上下文），注入位置改到**消息尾部**（与任务板同区），不插在重建投影与 system 之间（否则 meta 信号每次重启不同会破坏其后前缀缓存）

## 2. 阶段二 — R2/R3 任务跨重启接续 + fail-safe 对账

- [ ] 2.1 `tool/action/resident_recovery.go`：`ReattachResidentSessions` 重建 tmux 追踪后，连带从「重发现会话 + 持久 task 元数据」重建 TaskManager 板条目（可 resume/reattach）
- [ ] 2.2 `agent/task/task_manager.go`：`List()` 只读化——移除热路径 shell out / 状态变更 / onSettle 副作用；存活性对账抽为「启动期 + 限速后台 sweep」，不挂 BeforeModel
- [ ] 2.3 探测三态 fail-safe：`tmux` 命令失败 = unknown ≠ dead（保守保留）；判死经去抖确认窗（持续 dead 超窗才回收），单次失败不触发终局回收
- [ ] 2.4 状态一致性：`agent/event_bus.go` 补 `SettleFailed` 档渲染「✗ failed」；retire 带非 nil `Err` + 静默 detector/watch；`applyStatus` 加终态守卫（completed/failed/cancelled 不被复活为 stable）
- [ ] 2.5 回归：①tmux 不可达不误杀活会话（fail-before：现探测失败即判死）②`SettleFailed` 渲染 failed 非 completed（fail-before）③`List()` 只读无副作用 ④终态不被迟到 SettleStable 复活 ⑤启动重连连带重建板 ⑥retire 不产生重复 onSettle

## 3. 阶段三 — R4a 统一 config-watcher（threshold / tools）

- [ ] 3.1 泛化 `tool/mcp/registry.go:maybeSyncLocked` 为唯一 config-substrate watcher：tagent.yaml mtime → 重解析 → 逐段 diff → 分段 apply（section-scoped，复用 MCP registry 范式）
- [ ] 3.2 `config.go` 段级分类：可热应用（mcp_servers/prompt/tool-desc/evolution 已热 + 新增 compress_threshold/tools 增删）vs 需重启（memory backend、subagent 组合在阶段四前）；需重启段高可见日志 + fail-closed 保旧组合
- [ ] 3.3 threshold 单一原子权威：删 `cm.thresholdPct` 旁路字段，运维探针直读压缩器原子阈值（消除未同步共享写）
- [ ] 3.4 删死代码 `org_coordinator.go`（及 dev 侧并行 org 热重载闭包收敛进统一 watcher，不再有三套 mtime 机制）
- [ ] 3.5 fail-closed：重解析/diff 失败保留当前组合 + WARN，不崩溃、不降级到空组合
- [ ] 3.6 回归：①mtime 改 compress_threshold/tools 免重启生效 ②坏配置 fail-closed 保服务 ③单一原子权威、探针读权威值（fail-before：旁路字段分叉）④结构变更提示重启且保旧组合

## 4. 阶段四 — R4b subagent 热重组（硬骨头，后置）

- [ ] 4.1 subagent 段热应用：turn 边界安全换点（不在 BeforeModel 执行中途换组合）
- [ ] 4.2 in-flight 引用处理：正在执行的子 agent 工具调用以旧定义完成至该 turn 结束（drain），新组合下轮生效
- [ ] 4.3 回归：subagent 热改在 turn 边界生效 + in-flight 调用不中断 + 需重启段在未就绪前 fail-closed

## 5. 门禁与收尾

- [ ] 5.1 三道门禁全绿：`go build ./...` + `go vet ./...` + 全量 `-short` + 新/改子系统 `-race`（agent/compress、agent/task、tool/action、tool/mcp、prompt）
- [ ] 5.2 逐阶段回归 fail-before/pass-after 证据留存（每个 🔴/🟠 修复各有具名回归）
- [ ] 5.3 dev 协调（Open Question 4 裁决）：②③④ refactor-in-place on dev，或本变更 main 侧实现后 dev 合并 supersede
- [ ] 5.4 文档同步：`memory-architecture.md`（narrative 持久化 + 投影重建原语）、`platform-subsystems.md`、`agent-behavior-matrix.md`（重启连续性行为）、归档时 delta 合并入 `task-skeleton-compression` / `task-registry-and-board` 主 spec
- [ ] 5.5 `openspec validate resident-state-recompute --strict` 通过 + commit（conventional）+ archive 裁决
