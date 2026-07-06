## 1. memory_query / memory_recent 时间范围过滤

- [x] 1.1 `recallQueryArgs` 新增 `Since int64` 和 `Until int64` 字段（`json:"since,omitempty"` / `json:"until,omitempty"`）
- [x] 1.2 `recallRecentArgs` 新增 `Since int64` 和 `Until int64` 字段
- [x] 1.3 `NewRecallQueryTool` handler 将 `since`/`until` 映射到 `QueryOptions.StartTime`/`EndTime`
- [x] 1.4 `NewRecallRecentTool` handler 同样映射时间范围
- [x] 1.5 工具 Description 更新：声明 `since`/`until` 参数（Unix 毫秒时间戳）

## 2. memory_trace 因果链回溯工具

- [x] 2.1 新增 `recallTraceArgs` / `recallTraceResult` / `recallTraceItem` 类型定义
- [x] 2.2 实现 `NewRecallTraceTool(accessor)`：循环调用 `GetEvent(parentKey)` 沿链回溯，maxSteps 上限 20
- [x] 2.3 在 `buildRecallSubTools` 中注册 `memory_trace` 工具
- [x] 2.4 工具 Declaration：声明 `key`（必填）和 `max_steps`（可选，默认 10）

## 3. memory_get 父事件包含

- [x] 3.1 `recallGetArgs` 新增 `IncludeParent bool` 字段（`json:"include_parent,omitempty"`）
- [x] 3.2 `recallGetResult` 新增 `Parent *parentEventInfo` 字段（含 EventKey/EventType/EventSummary/Timestamp）
- [x] 3.3 `NewRecallGetTool` handler：当 `include_parent=true` 且 `ParentKey != 0` 时，调 `GetEvent(parentKey)` 取父事件摘要填入 `Parent` 字段
- [x] 3.4 工具 Description 更新：声明 `include_parent` 参数

## 4. 构建验证与回归

- [x] 4.1 `go build ./...` 全量编译通过
- [x] 4.2 `go test ./...` 全量测试通过（无回归）
- [x] 4.3 现有 recall 相关测试不受影响（向后兼容验证）
