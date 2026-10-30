# 03 技术设计

## 文件清单

- `memory/replay/age_segments.go`：`splitSegments` 判据、进行中段定义。
- `memory/replay/age_plan.go` / `age_segments_test.go`：判据测试。
- `openspec/specs/task-skeleton-compression/spec.md`（由 F3 同步）。

## 技术要点

### 1. 段判据

按链序遍历，维护两个集合：

- `pending`：已到达但尚未被任何回答携带的输入键；
- `closed`：已闭合段的输入与会话。

遇到 `agent_output`：

- 读取其 Meta `MetaKeyConsumedInputs`；
- 把 `pending ∩ consumed` 的输入、以及它们之间的过程条目，闭合成一段；
- `pending \ consumed` 继续留在进行中段（B 不被 A 的回答闭合）。

遇到其他输入（`external_input` / 通知材料 / 工具结果）：加入 `pending`（工具结果不开启新段，只作为过程材料）。

### 2. 进行中段

尾部未闭合部分 = 进行中段；`LevelOfAge` 恒 L0，永不折叠。这与既有「进行中段恒 L0」一致，只是判据从「无 agent_output」变为「有未被消费的输入」。

### 3. 兼容旧链

`agent_output` 缺 `MetaKeyConsumedInputs` 时按旧判据（链序闭合）解释，并在诊断面标记 `legacy_segment=true`；新写入一律带 Meta。

### 4. 重建

段由链上事实与 Meta 纯函数推导，无额外持久结构；重启后 `seedReplay` 后同一 `splitSegments` 得到相同段。

## 验证

- 正锚：`TestUnconsumedInputStaysInProgress`（B 不被 A 的回答闭合，A 段只含 A）。
- 负锚：`TestConsumedInputClosesItsSegment`（A 被其回答闭合）。
- 消费者锚：`ReplayEntries` 正式读面 + `requestAssembly` 的投影段。
- 域内：`go test ./memory/replay/... -count=1`；`event-short` 十轮。

## 风险与回退

- 判据改动影响 fold 与呈现；先落 `TestUnconsumedInputStaysInProgress` 红锚，再改判据。
- 回退：单文件还原 `age_segments.go`；症状锚 3（B 不被闭合）回滚后必红。
