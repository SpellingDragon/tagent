# Tasks: 10-turn-segment

- [x] 10.1 复核消费关系归段既有 pin 绿 —— 验证：`go test ./memory/replay/ -run TestSegmentsCloseByConsumptionRelation -count=1` + `go test ./tests/integration/host/ -run TestProjectionFoldsByTheWiredConsumptionRelation -count=1`
- [x] 10.2 重启同段 pin：seedReplay 后段边界一致 —— 验证：`go test ./assembly/ -run TestRestartSameSegmentAfterReplay -count=1`
- [x] 10.3 诊断面 legacy 只读访问器（不扩 ProjectionEntry 公开面） —— 验证：正 `go test ./memory/replay/ -run TestSegmentLegacyMarker -count=1` + 负（公开面仍五字段）`go test ./memory/replay/ -run TestProjectionEntryFaceUnchanged -count=1`