# maintainability-audit — 任务

## 1. 审计准备（base 锚定与取证基线）

- [x] 1.1 锚定 base commit（main HEAD，写入 summary.md 头部），确认工作区洁净无未合入改动
- [x] 1.2 全量取证基线：`go vet ./...`、`go test ./... -short`、`go test ./agent/... -race`、关键包 `-cover`，输出存 `audit/baseline.md`（含原始命令与结果摘要）
- [x] 1.3 生成包/文件清单与行数表（`go list ./...` + wc），作为逐包任务的对账底册
- [x] 1.4 建立发现台账骨架 `audit/summary.md`（F-xx 编号规则、三级分类列、证据列、来源列、立案形态列）与 backlog 骨架 `audit/backlog.md`

## 2. 叶子包审计（无下游依赖）

- [x] 2.1 `event` 包：逐文件审阅 types/metadata/registry/timeline；核对 EventTypeSpec 注册表与 14 类型注释一致性；发现入账
- [x] 2.2 `prompt` 包：逐文件审阅 loader/source；核对 LoadFiles skip-missing 语义与 wiki/specs 表述；发现入账

## 3. memory 族审计（四包）

- [x] 3.1 `memory`（核心包）：kv.go/engine.go/embedder.go 契约、segment_store、in_memory_store、consolidation、feedback、error_tracking、lifecycle；逐文件审阅 + 评分卡
- [x] 3.2 `memory/engine`：engine_bridge、engine_inmemory（hybrid RRF）、diagnostics；逐文件 + 评分卡
- [x] 3.3 `memory/kv`：localfile、rustviking 后端；降级矩阵与实现一致性；逐文件 + 评分卡
- [x] 3.4 `memory/embedder`：zhipu/mock/traced；评分卡（小包可并简报）

## 4. plugin 包审计

- [x] 4.1 `plugin`：MemoryPlugin（持久化+因果+同点投影）、SummaryPlugin、attribution、projection_sink、spill 重放双写；逐文件审阅 + 评分卡

## 5. agent 族审计（五包）

- [x] 5.1 `agent/task`：task_manager（Spawn/watch/resume/RestoreTask/三对账/pruneTerminal）、task_board、fixture；含 pruneTerminal 修复后语义复查；逐文件 + 评分卡
- [x] 5.2 `agent/compress`：smart_compress、context_compressor、compaction_event、projection、task_segmenter、token_counter；逐文件 + 评分卡
- [x] 5.3 `agent/governance`：gate/tool/budget/approval/ledger/classifier/goal；逐文件 + 评分卡
- [x] 5.4 `agent/reliability`：DegradationManager、SpillStore/ReliableBus、AnchorStore；逐文件 + 评分卡
- [x] 5.5 `agent` 本体：agent.go、event_loop、event_bus、context_manager（含 R4 executorMu/SwapExecutor/orgReloader）、lifecycle、trace、meditation、tool_agent、projection_rebuild、task_record_sink、governance、replay_restore；逐文件 + 评分卡（本变更最大件，允许拆分会话）

## 6. tool 族审计（六包）

- [x] 6.1 `tool/action`：action_tool、declarative、resident_recovery、tmux_executor、tmux_monitor、settle、poll_schedule；逐文件 + 评分卡（最大 tool 包，允许拆分）
- [x] 6.2 `tool/task` + `tool/govx`：任务工具族与治理面五件套；合并简报 + 双评分卡
- [x] 6.3 `tool/mcp`：Registry 热同步 + mcp_call 网关；逐文件 + 评分卡
- [x] 6.4 `tool/memoryx`：memory_consolidate、memory_health；评分卡
- [x] 6.5 `tool/plan`：PlanAgent 交互契约；评分卡

## 7. 独立子系统审计（两包）

- [x] 7.1 `evolution`：GitEvolution、gitrefine、refine 工具、judge/guardrail；逐文件 + 评分卡
- [x] 7.2 `rl`：TrajectoryRecorder、SwappableModel、HTTPAPI；逐文件 + 评分卡

## 8. 根包审计（组合根）

- [x] 8.1 `tagent.go`（Option/New）+ `wiring.go`（resolve/wire 族）：逐文件 + 评分卡
- [x] 8.2 `build_agent.go`（buildMode/buildRunner/ownership 谓词/装配分支）：逐文件 + 评分卡；复查 buildMode 类型化后残留裸 bool 或注释式规则
- [x] 8.3 `config.go` + `testing.go` + `org_hotreload.go`：配置面/测试助手/热更编排；逐文件 + 评分卡

## 9. 横切第二遍（交叉主题）

- [x] 9.1 错误处理一致性横切：错误包装/日志级别/静默吞错跨包抽查，发现归所属包台账
- [x] 9.2 锁与并发纪律横切：跨包无锁字段访问、锁内回调、goroutine 泄漏面抽查；对照 baseline race 结果定性
- [x] 9.3 specs 一致性横切：按包抽核主 specs SHALL 与实现（含 c5399e2 状态注记本身的时效）；漂移入账
- [x] 9.4 死代码/悬空注释横切：grep 残留（退役机制名、TODO/FIXME、指向已删符号的注释）入 🟡 账

## 10. 汇总与收尾

- [x] 10.1 完成汇总台账 `audit/summary.md`：F-xx 全量发现按严重级排序、来源标注（存量/引入）、反证记录保留
- [x] 10.2 完成 `audit/backlog.md`：按包归组、引用 F-xx、标注立案形态与「不修的后果」、🔴 级标优先窗口
- [x] 10.3 fresh-eyes 复审：随机抽 3 包评分卡 + 5 条发现，第二人按 file:line/符号引用复核可验证性
- [x] 10.4 `openspec validate --strict` 通过；LEDGER 回写审计结论摘要与 backlog 规模
