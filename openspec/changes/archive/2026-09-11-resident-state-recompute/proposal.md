> **⚠️ 归档裁决（2026-09-12）**：本变更经 fresh-eyes 三路复验对照 dev 真实代码，**设计不成立**（R1 recompute-from-WAL 做不到逐字节、压缩视图 path-dependent 非 WAL 纯函数；R2/R3 奉为范本的 resident_recovery 生产是死代码 + task 板零持久化；R4 泛化 maybeSyncLocked 到 tools/subagent 是范畴错误），**未实现（tasks 0/25）**，归档为设计决策记录。修正方向 + 独立成立的真 dev bug 清单见 design.md 顶部「归档裁决」。delta specs **不并入主 specs**（--skip-specs）。

## Why

dev 分支的三个「常驻连续性」特性（压缩事件溯源 replay、task 僵尸对账、org 配置热重载）各自**重造或违背了 main 已有的两个协调范式**——`resident_recovery.go`（重启期从持久基底重算视图）与 MCP registry / prompt.Source 的 mtime 热同步——因而引入了平行真相源（压缩快照）、查询热路径上的有副作用对账、以及第三套并行热重载。本变更把 main 自有范式**统一推广**到全部常驻运行态：运行态是持久基底的可重算物化视图，只持久化不可重算的那一小块，在触发点（重启 / 配置变更）重算，绝不快照整个视图、绝不在查询热路径对账。

## What Changes

- **R1 对话上下文跨重启连续**：把唯一不可重算的 LLM 滚动综述（narrative）持久化为 `context_compress_summary` 正 key WAL 事件（**仅此一块逆转** `task-skeleton-compression` 当初的固化物移除——按减法判定标准它属「缺失补最小量」，非冗余）；新增启动期 `RebuildProjectionFromWAL`（与 `ReattachResidentSessions` 结构同构）重放 WAL → 投影逐字节重建 → 前缀缓存复用。**BREAKING**（对 dev ② 快照方案）：移除 `persistSnapshotEvent` / `compress/snapshot.go` / `ReplayProjectionHandler` 的 Replace 快照分支。
- **R2/R3 任务跨重启接续 + 超长/交互异步命令**：task 板存活性对账**折叠进启动期 `ReattachResidentSessions`**（+ 可选限速后台 sweep），不再挂在 `List()` 热路径；`List()` 回归只读内存快照；探测**三态 fail-safe**（tmux 命令失败 = unknown ≠ dead，不误杀活会话）；`SettleFailed` 在 event_bus 正确渲染为 failed（非 "✓ completed"）；retire 静默 detector + applyStatus 终态守卫。
- **R4 配置描述的全量运行时热更新**：把 MCP registry 的 `maybeSyncLocked` 泛化成**唯一 config-substrate watcher**（tagent.yaml mtime → 重解析 → 逐段 diff → 分段 apply），覆盖已热的 mcp_servers/prompt/tool-desc/evolution + **新增** compress_threshold/tools 增删/subagent 重组；每段分「可热应用 vs 需重启」；安全换点在 turn 边界；单一原子权威（删 `thresholdPct` 旁路字段）；删死代码 `org_coordinator.go`。
- 三处 dev 分歧实现（②快照 / ③热路径对账 / ④并行热重载）收敛回 main 自有范式，不再平行存在。

## Capabilities

### New Capabilities
- `projection-restart-rebuild`: 启动期从完备 WAL 重放重建压缩投影（`RebuildProjectionFromWAL`），与 `ReattachResidentSessions` 同构；只用幂等 Append、不 Replace 活投影；确定性重折叠复原逐字节上下文供前缀缓存复用。
- `resident-config-hot-sync`: 单一 config-substrate 观察者，泛化 MCP registry 的 mtime→逐段 diff→分段 apply 到全部运行时段（tools/MCP/subagent/threshold/prompt），turn 边界安全换点、fail-closed 保旧值、每段热应用 vs 需重启分类。

### Modified Capabilities
- `task-skeleton-compression`: 逆转「`context_compress_summary` 固化物产生源已移除」——但**仅限不可重算的 LLM 滚动综述**（正 key、TTL 豁免、可召回）；卡片行等可重算产物仍不落库、走 `[evt_key]` 票据。narrative 成为 WAL 完备性的一部分。
- `task-registry-and-board`: 存活性对账从「`List()` 热路径有副作用」改为「启动期 Reattach + 限速后台 sweep」；`List()` 只读；探测三态 fail-safe（unknown≠dead）；`SettleFailed` 正确渲染；retire 静默 detector + 终态守卫防重复通知/终态复活。

## Impact

- **代码**：`agent/replay_restore.go`（删 Replace 分支，保留 Append/meditation）、`agent/context_manager.go`（删 persistSnapshotEvent）、`agent/compress/snapshot.go`（删）、`agent/compress/context_compressor.go`（narrative 折叠点落 `context_compress_summary`）、`agent/task/task_manager.go` + `agent/event_bus.go`（对账折叠 + SettleFailed 档 + 终态守卫）、`tool/action/resident_recovery.go`（扩展连带重建 task 板）、`tool/mcp/registry.go`（泛化为统一 watcher）、`tagent.go`/`build_agent.go`（启动重建接线 + 统一 watcher 接线）、`config.go`（段级热应用分类）；dev 侧 `org_coordinator.go`/`org_hotreload.go` 删除或折叠。
- **存储**：重新产生 `context_compress_summary` 正 key 事件（TTL 豁免、Recallable+Embeddable）→ recall 面变化（召回返回真综述而非票据噪音）；narrative 滚动 supersede（墓碑旧的）控增长。
- **运行时**：新增启动期投影重建（O(活事件)，一次性）；新增统一 config watcher（复用既有 mtime 惰性检查）。
- **分支协调**：本变更是 dev ②③④ 的协调替代，实施须与 dev 活跃 dogfood 协调（supersede 或 refactor 收敛）。
- **消除的缺陷**：🔴 快照 Replace 抹活投影 / fullBoundary 跨 goroutine race / 写入常开无门控；🔴 探测失败屠杀看板 / SettleFailed 误报 completed；🟠 recall 污染 / threshold 域 / boundary=0 误判 / org 死代码。
