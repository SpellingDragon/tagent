## 任务清单

本变更分为 4 个阶段，每阶段独立可验证。外包实现时按阶段交付和验收。

---

## 阶段 1: 删除 EventValuator 和 ChunkSplitter（减法）

> 目标: 移除不必要的复杂性，使 SmartCompressor 编译通过但不调用被删模块

### Task 1.1: 删除 event_value.go

- [x] 删除文件 `agent/event_value.go`
- [x] 删除文件中定义的所有类型: `EventValuator`, `EventValue`, `ValuationConfig`, `DefaultValuationFloors`, `referenceValuator`, `noopValuator`
- [x] 搜索全项目 `grep -rn "EventValuator\|EventValue\|ValuationConfig\|WithEventValuator\|WithValuationConfig\|valuationConfig\|eventValuator" --include="*.go"` 并修复所有编译错误

### Task 1.2: 删除 chunk_splitter.go

- [x] 删除文件 `agent/chunk_splitter.go`
- [x] 搜索全项目 `grep -rn "ChunkSplitter\|chunkSplitter\|chunkSize\|chunkSummaryLen\|WithChunkSize\|WithChunkSummaryLen" --include="*.go"` 并修复所有编译错误
- [x] 从 SmartCompressor 结构体中移除 `chunkSplitter`, `chunkSize` 字段（保留 `chunkSummaryLen` 作为截断限制）

### Task 1.3: 清理 SmartCompressor 中的 valuation 引用

- [x] 在 `agent/smart_compress.go` 中:
  - 移除 `eventValuator` 字段和相关 Option 函数
  - 移除 `valuationConfig` 字段和相关 Option 函数
  - 移除 `archiveCache` 字段（归档时不再做内容哈希去重）
  - 移除 Compress 方法中"阶段: evaluate segments via EventValuator"的代码块
  - 用确定性分级逻辑替代价值评估（合并了 Task 2.1 和 2.2）

### Task 1.4: 清理配置

- [x] 在 `config.go` 的 `CompressConfig` 中:
  - 删除 `ValueFloors map[string]float64` 字段
  - 删除 `ValuationTimeoutMs int` 字段
  - 删除 `ChunkSize int` 字段
- [x] 在 `agent/tagent_agent.go` 的 `CompressConfig` 中同步删除
- [x] 在 `tagent.go` 和 `agent/tagent_agent.go` 中移除对这三个字段的引用
- [x] 更新 `examples/wechat-bot/tagent.yaml`: 删除 `value_floors` 和 `valuation_timeout_ms` 配置行

### Task 1.5: 验证阶段 1

- [x] `go build ./...` 无编译错误
- [x] `go test ./agent/ -count=1` 全部通过（删除了 `event_value_test.go`、`batch_summary_test.go`、`chunk_splitter_test.go`、`archive_rag_test.go`，修复了 `context_compressor_test.go` 和 `smart_compress_test.go` 的引用）
- [x] `go test ./... -short -count=1` 全部通过

---

## 阶段 2: 实现确定性分级函数（核心替换）— 已在阶段 1 中合并完成

> 目标: 用 deterministicLevel 替代被删除的 LLM 评估（已在 Compress 方法中内联实现）

### Task 2.1-2.4: 已完成（内联到阶段 1）

- [x] 确定性分级逻辑已内联到 Compress 方法中（非独立函数，直接在循环中判断）
- [x] `go build ./...` 无编译错误
- [x] `go test ./agent/ -count=1` 全部通过
- [x] `go test ./tests/ -short -count=1` 全部通过

---

## 阶段 3: 清理 SmartCompressor 死代码（收尾减法）

> 目标: 移除阶段 1 遗留的不再使用的函数（1383→1195 行）

### Task 3.1: 删除死代码 buildReferenceMessage

- [x] `buildReferenceMessage` 函数已无调用方（grep 确认仅定义无引用），删除

### Task 3.2: 移除 Compress 注释中的过时描述

- [x] 更新 Compress 方法顶部的 Algorithm 注释:
  - 移除"2. Evaluate segments via EventValuator"
  - 移除"3. Plan compression: sort by value density"
  - 替换为当前实际流程: "2. Deterministic level assignment based on age + content"

### Task 3.3: 移除 sort import

- [x] `"sort"` import 已在阶段 1 中移除（阶段 1 删除了 `sort.Slice` 调用）
- [x] 运行 `goimports` 或手动确认

### Task 3.4: 验证阶段 3

- [x] `go build ./...` 无编译错误
- [x] `go vet ./agent/` 无警告
- [x] `wc -l agent/smart_compress.go` 确认 1195 行（≤1200 行目标达成）
- [x] `go test ./agent/ -count=1` 全部通过

---

## 阶段 4: 实现空闲期投影整理（新增能力）

> 目标: 让 Agent 在空闲时主动整理投影，减少紧急压缩的触发频率
>
> 设计调整: 不引入 PullWithTimeout（会改变事件循环的纯事件驱动语义）。
> 改为复用现有的定时器机制——类似 MeditationManager 的独立 goroutine，
> 定期检查 Projection 并直接更新 ref 的 EventSummary。

### Task 4.1: 实现 ProjectionOrganizer

- [x] 新建文件 `agent/projection_organizer.go` (299 行)
- [x] 实现 `ProjectionOrganizerConfig` 配置结构体
- [x] 实现 `NewProjectionOrganizer(cfg, projection, memStore, lastEventTime func() int64)`
- [x] 实现 `Start()` — 启动定时器 goroutine（summaryModel 为 nil 时不启动）
- [x] 实现 `Stop()` — 停止并等待
- [x] 实现 `OrganizeOnce(ctx context.Context) int`:
  1. 检查 lastEventTime，距现在 < minIdleGap 则跳过
  2. `refs := projection.GetAll()`
  3. 从前往后扫描，找到 age > organizeAge 且 EventSummary 长度 > maxSummaryLen 的 refs
  4. 对每个待整理 ref，从 MemoryStore 获取完整内容
  5. 调 summaryModel 生成精炼摘要（≤maxSummaryLen 字符）
  6. 通过 `projection.UpdateSummary(idx, newSummary)` 更新
  7. 检查 ctx.Err() — 取消则立即返回
  8. 返回整理计数
- [x] 输出结构化日志: `[OrganizeProjection] organized=N skipped=M failed=F`

### Task 4.2: SessionProjection 新增 UpdateSummary

- [x] 在 `agent/projection.go` 中新增 `UpdateSummary(idx int, summary string)` 方法

### Task 4.3: 集成到 TagentAgent

- [x] 在 TagentAgent 结构体中新增 `organizer *ProjectionOrganizer` 字段
- [x] 在 `NewTagentAgent` 中，如果 `cfg.SummaryModel != nil`，创建 Organizer（共享 MeditationManager 的 `lastEventTime`）
- [x] 在 `StartLoop` 中调用 `ta.organizer.Start()`
- [x] 在 `StopLoop` 中调用 `ta.organizer.Stop()`
- [x] 复用 `MeditationManager.lastEventTime`（通过新增 `LastEventTime()` 公开方法）

### Task 4.4: 单元测试

- [x] 新建 `agent/projection_organizer_test.go` (9 个测试):
  - TestProjectionOrganizer_OrganizeOnce — 正常整理 + batchSize 限制
  - TestProjectionOrganizer_SkipsShortSummaries — 跳过已精炼的短摘要
  - TestProjectionOrganizer_SkipsCompressType — 跳过 context_compress 类型
  - TestProjectionOrganizer_CtxCancellation — ctx 取消时立即返回
  - TestProjectionOrganizer_NotEnoughRefs — refs 不足时跳过
  - TestProjectionOrganizer_StartStop — goroutine 生命周期
  - TestProjectionOrganizer_NilModel — summaryModel 为 nil 时不启动
  - TestProjectionOrganizer_LLMErrors — LLM 失败时不更新
  - TestUpdateSummary_OutOfBounds — 越界时静默返回

### Task 4.5: 验证阶段 4

- [x] `go build ./...` 无编译错误
- [x] `go test ./agent/ -run "TestProjectionOrganizer" -v` 全部 9 个测试通过
- [x] `go test ./agent/ -count=1` 全部通过
- [x] `go test ./... -short -count=1` 全部通过
- [x] `tests/invariants_test.go` 通过

---

## 最终验证

- [x] `go build ./...` 无错误
- [x] `go vet ./...` 无警告
- [x] `go test ./... -short -count=1` 全部通过（排除需要真实 LLM 的集成测试）
- [x] `wc -l agent/smart_compress.go agent/projection_organizer.go` 确认: smart_compress 1195 行 + organizer 299 行
- [x] 运行 WeChat Bot 示例配置解析无报错: `cd examples/wechat-bot && go build .`
