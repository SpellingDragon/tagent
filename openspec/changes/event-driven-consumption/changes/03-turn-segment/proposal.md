# 03-turn-segment — 分段闭合

父变更：`event-driven-consumption`（依赖 01 的消费关系）。

## 边界与依赖

- **依赖**：01 产出的消费关系（`agent_output` Meta 的 `MetaKeyConsumedInputs`）。
- **被依赖方**：04 派生读面（段与导出配对）、压缩/折叠（`task-skeleton-compression` 判据）。
- **接口面**：`splitSegments` 的判据、进行中段定义、回滚重建路径。
- **禁止事项**：不改 `runtime/agent`（01 所有）；不改事实族；不给段新增持久结构（消费关系已在输出事实里）。

## Why

`splitSegments` 只按链序遇到 `agent_output` 闭合（`memory/replay/age_segments.go`）。A/B 场景下链序是「A → B → 对 A 的回答」，按现判据 B 会被并入已完成的 A 段；而 B 实际未消费。

## What Changes

1. 段闭合依据改为「本次回答实际携带的输入」（来自输出事实 Meta）。
2. 未被任何输出携带的输入保持「进行中段」，永不折叠。
3. 段判据可从链重建：重启后按同一 Meta 重建段，无需额外持久面。

## Risk

- 既有段测试与 golden 场景按新判据迁移；`task-skeleton-compression` 规格同步。
- 旧链（无 Meta）需回退到纯链序判据，且仅用于读取历史，不用于新写入。
