# Design: 10-turn-segment

legacy 标记以 memory/replay Segment 只读访问器落地（不扩 ProjectionEntry 公开面）；重启同段 pin 放 assembly/facts_test.go（seedReplay 后段边界一致）。回退：单域回滚，只读访问器无行为变化。