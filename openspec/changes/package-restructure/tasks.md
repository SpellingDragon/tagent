## 1. 创建 contextmgr 包

- [ ] 1.1 新建 `contextmgr/` 目录
- [ ] 1.2 移动 `agent/context_manager.go` → `contextmgr/context_manager.go`，修改 package 和 import（添加 `agent` 包引用）
- [ ] 1.3 移动 `agent/smart_compress.go` → `contextmgr/smart_compress.go`，修改 package 和 import
- [ ] 1.4 移动 `agent/task_segmenter.go` → `contextmgr/task_segmenter.go`，修改 package 和 import
- [ ] 1.5 从 task_segmenter 中拆出 Compactor 到 `contextmgr/compactor.go`（Compactor 类型 + NewCompactor + Compact 方法 + buildSummaryReference）
- [ ] 1.6 移动 `agent/chunk_splitter.go` → `contextmgr/chunk_splitter.go`，修改 package
- [ ] 1.7 移动 `agent/plan_progress_tracker.go` → `contextmgr/plan_progress_tracker.go`，修改 package 和 import
- [ ] 1.8 移动对应测试文件到 contextmgr 包：context_manager_test.go, smart_compress_test.go, task_segmenter_test.go, chunk_splitter_test.go, plan_progress_tracker_test.go, compact_test.go, batch_summary_test.go, compress_usermsg_test.go
- [ ] 1.9 `go build ./contextmgr/...` 通过

## 2. 更新 agent 包引用

- [ ] 2.1 在 `agent/tagent_agent.go` 中添加 `contextmgr` import，更新对 ContextManager/SmartCompressor/Compactor/TokenCounter/TaskSegment/SegmentMessages/SmartCompressorOption/PlanProgressTracker 的引用为 `contextmgr.` 前缀
- [ ] 2.2 确认 `newContextManagerFromConfig` 调用 `contextmgr.NewContextManager`、`contextmgr.NewSmartCompressor`、`contextmgr.NewCompactor` 等
- [ ] 2.3 确认 `NewTagentAgent` 中 `contextmgr.NewDefaultTokenCounter()` 调用
- [ ] 2.4 确认 `FrameworkPrompt` 留在 agent 包且已导出
- [ ] 2.5 移动 agent 包内引用了已移出类型的测试文件中的引用更新
- [ ] 2.6 `go build ./agent/...` 通过

## 3. 更新根包引用

- [ ] 3.1 更新 `tagent.go` 中对 contextmgr 类型的引用（如 `agent.CompressConfig` 保持不变，但 `SmartCompressor` 等如被引用则改前缀）
- [ ] 3.2 确认 `tagent.go` 中 `buildCompressorOpts` 返回 `[]contextmgr.SmartCompressorOption`
- [ ] 3.3 更新 `builtin.go` 中引用（如有）
- [ ] 3.4 更新 `registry.go` 中引用（如有）
- [ ] 3.5 `go build ./...` 通过（无 import cycle）

## 4. 最终验证

- [ ] 4.1 `go build ./...` 全部通过
- [ ] 4.2 `go test ./agent/... ./contextmgr/... ./plugin/... ./event/... ./memory/...` 全部通过
- [ ] 4.3 确认 agent 包源文件数 = 8（tagent_agent, event_bus, projection, meditation, tool_agent, output_limit_tool, trajectory_recorder, http_api）
- [ ] 4.4 确认 contextmgr 包源文件数 = 6（context_manager, smart_compress, task_segmenter, compactor, chunk_splitter, plan_progress_tracker）
