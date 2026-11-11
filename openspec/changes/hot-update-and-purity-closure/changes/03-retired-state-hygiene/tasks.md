# Tasks: 03-retired-state-hygiene

- [x] 3.1 删 publicationRecords.records 影子只写副本 —— 验证：`grep -c "records" assembly/commit.go` 影子零残留 + `go build ./...` + `go test ./assembly/ -run TestPublicationNoShadowCopy -count=1`（载体更正 r1-1：真测试在 assembly/retired_state_hygiene_test.go）
- [x] 3.2 configFact 收敛单一 wire 编码 —— 验证：正 `go test ./assembly/ -run TestConfigFactSingleEncoding -count=1` + 负（旧事实可读）`go test ./assembly/ -run TestConfigFactLegacyReadable -count=1`（载体更正 r1-1）
- [x] 3.3 撤 capture_budget_mb 死参 budgetMB+死分支 —— 验证：`grep -c budgetMB assembly/` =0 + `go build ./...` + 负 `go test ./assembly/ -run TestCaptureNoBudgetParam -count=1`
- [x] 3.4 撤 SetRetrievalTuning 独立写权改派生/常量 —— 验证：`go test ./memory/search/ -run TestRetrievalTuningDerived -count=1` + grep 独立写零
- [x] 3.5 快照 Cursor 清理 + pinnedFacts boot 重播种 —— 验证：`go test ./runtime/state/ -run TestRecoveryCursorReadOrRemoved -count=1` + `go test ./assembly/ -run TestPinnedFactsReseeded -count=1`