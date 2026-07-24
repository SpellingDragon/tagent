## 1. 移除 ProjectionOrganizer

- [x] 1.1 删除 `agent/projection_organizer.go` 及其测试 `agent/projection_organizer_test.go`（若存在）
- [x] 1.2 `agent.go`：移除 `organizer *ProjectionOrganizer` 字段、`cfg.SummaryModel != nil` 分支内的 `NewProjectionOrganizer(...)` 装配（**保留**同分支的 `WithSummaryModel` → SmartCompressor Stage-2）
- [x] 1.3 `lifecycle.go`：移除 `ta.organizer.Start()` / `Stop()` 两处调用

## 2. 清理与验证

- [x] 2.1 全局搜索无 `ProjectionOrganizer` / `organizer` 残留引用；`go build ./...` 通过
- [x] 2.2 `go vet ./agent/`；确认 `summary_model` 仍正确注入 SmartCompressor（Stage-2 按需可用）
- [x] 2.3 回归：`go test ./agent/ -short -count=1` 全绿；确认无 organizer 相关测试遗留导致的编译/失败

## 3. 收尾

- [x] 3.1 `scripts/check-openspec.sh` 通过；`openspec validate retire-projection-organizer --strict` 通过
- [x] 3.2 按 Conventional Commits 提交（含 REMOVED delta；归档时移除 `openspec/specs/projection-organize/`）
