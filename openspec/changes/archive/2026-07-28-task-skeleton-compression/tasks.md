# task-skeleton-compression — 实现任务

## 1. 段边界重构（task_segmenter.go）

- [x] 1.1 新增 `agent_output` 识别helper：优先经 event 前缀（`ParseEventKeyAndType` → `agent_output`）判定，缺失前缀时退回启发式（`assistant` 且 `len(ToolCalls)==0`）
- [x] 1.2 重写 `SegmentMessages` 边界判定：遇 `agent_output` 闭合当前段；连续 `external_input` 归入同一进行中段；无 `agent_output` 的尾部标记 `IsComplete=false`
- [x] 1.3 新增段内二分：按事件类型纯函数区分骨架（`external_input`/`agent_output`）与中间事件（`action_command`/`thinking_plan`），不读消息内容
- [x] 1.4 单测：完整回合闭合、连续 external_input 归并、无输出尾部 IsComplete、缺前缀启发式兜底

## 2. 丢弃与定级重构（smart_compress.go）

- [x] 2.1 以骨架二分 + `tool>assistant` 丢弃序替换 `selectiveSplit`（key/nonKey 启发式）
- [x] 2.2 重写定级函数：agent_output 段龄驱动（L0 完整 / L1 丢 tool / L2 仅骨架 / L3 多段压缩），废弃 `HasUserInput` 判据
- [x] 2.3 进行中段（`IsComplete=false`）恒 L0，全部消息完整保留
- [x] 2.4 组装逻辑确保所有保留消息携带原 event key 前缀（`[evt_KEY|type]`）
- [x] 2.5 单测：定级表全档、`tool>assistant` 丢弃序、进行中段完整保留

## 3. 多段压缩归档出口

- [x] 3.1 实现 L3 多段压缩：老骨架段整段不进入 `compressedMsgs`（其 event key 不出现），整段移出时间线
- [x] 3.2 衔接 `buildRetainedRefs`：确认被压骨架段的 `external_input`/`agent_output` 经 `extractCardLine` 汇入 rolling summary
- [x] 3.3 零 LLM 兜底：`summaryModel=nil` 时多段压缩走 engineering 卡片，不失败不降级
- [x] 3.4 单测：老段归档进 rolling summary、段数随轮次收敛、无摘要模型走通、recall key 可溯源

## 4. 性能与开关

- [x] 4.1 `resolveRef` 接入 `recentFullCount`：最老 ref 用 `EventSummary` 替代全量 `GetEvent()`，收敛 BeforeModel 的 store 查询规模
- [x] 4.2 新增 `WithSkeletonSegmentation(bool)` 开关，可按配置回退旧 user 切段
- [x] 4.3 配置接线：`config.go` / `agent.go` / `tagent.go` 增加 skeleton 分段开关与 recentFullCount（recentFullCount 接线已存在，本次接通 resolveRef 消费侧）

## 5. 验证与回归

- [x] 5.1 生产 session 回放：确认段数随轮次收敛、L3/多段压缩级别真实触发、`external_input` 进入 rolling summary（对照旧日志 L2:12→61）（开发环境无生产 session 数据，以 20 轮合成回放测试 `TestContextCompressor_SegmentCountConverges` 验证同三项断言；真实生产回放待上线后观察 skeleton 模式日志）
- [x] 5.2 全量测试通过 + 构建干净（无回归）
- [x] 5.3 同步压缩模块 wiki（段模型、定级、多段压缩归档出口）

## 6. Code Review 优化项（交付）

> 来源：code review（0 Blocker / 0 Major / 3 Minor / 2 Nit）。核心实现（1–5 组）已完成并通过测试；本组为合入前/跟进优化，决策见 design D6/D7。

- [x] 6.1 **M1 性能**：`compressSkeleton` 预算升级 O(n²)→O(n)——预计算 `cost[i][0..3]`（4n 次 `Estimate`）+ `systemCost`，L2/L3 升级改为 O(1) 增量（`total -= cost[i][old] - cost[i][new]`），消除内层 `estimate()` 重算
- [x] 6.2 **M2 保真**（已决 D6）：`recentFullCount` 未显式配置时默认取 `keepRecent × DefaultRefsPerTurn`（`DefaultRefsPerTurn=4`），使最近 `keepRecent` 个完整回合整体全量；显式配置（`WithRecentFullCount`/`recent_full_count`）优先；接线 `config.go`/`agent.go`/`tagent.go`
- [x] 6.3 **M3 巨段拆分补测**（已决 D7）：为 `generateSummaryRecursive` 补单测——超限触发消息组二分段拆分、单条超限 as-is 不截断（legacy 路径）
- [x] 6.4 **N1 注释**：`context_compressor.go` 配对合法性注释由“single place”改为“渲染期处理存量悬空 + 压缩期 L1 处理本轮丢弃所致的悬空”两层职责
- [x] 6.5 **N2 估算**（可选）：`tokenCounter.Estimate` 空集特判返回 0（消除 L3 移出段每段 +1 token 的保守偏差）
- [x] 6.6 **测试补强**：① 端到端 render-legality——多轮含真实 tool_call/result 对、过预算压 L1 后对最终 `Messages` 断言无 dangling call / 无孤儿 result；② 孤儿 ref 断言——某 L1 被丢的 `action_command` key 既不在 `RetainedRefs`、又进了 rolling summary 的 `recent keys=`
- [x] 6.7 **验证**：`go build ./...` + `go test ./agent/compress/... -count=1` 全绿；同步压缩模块 wiki（recentFullCount 挂钩、巨段拆分、O(n) 升级）
