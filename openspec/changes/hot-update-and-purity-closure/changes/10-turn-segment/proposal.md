# Proposal: 10-turn-segment

父变更：hot-update-and-purity-closure（转移自 event-driven-consumption 03；delta 正文已由 dev 170ef267 兑现）

## Why

消费关系归段主体已实现（memory/replay/age_segments.go splitSegments/closeByConsumption；loop.go 写 input_events；facts.go mappedEntryOf 接线），既有 pin 绿。残差二：重启同段无 pin；诊断面 legacy 标记未实现。

## 边界与依赖

- 依赖：01 消费元数据（已done）；接口常数=ProjectionEntry 五字段公开面（不改）。
- 被依赖方：06（replay 读缝）。
- 接口面：memory/replay/age_segments.go、assembly/facts_test.go。
- 禁止事项：不改 ProjectionEntry 公开五字段；不改 assembly/facts.go 生产面；不新增公共诊断 API（legacy 用 memory/replay 只读访问器）。