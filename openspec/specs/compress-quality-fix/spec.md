# compress-quality-fix Specification

## Purpose

投影按事件键幂等：同一 canonical 事实只进入投影一次，无键条目不去重。这是 compress-quality-fix 能力中仍现行的行为契约；其余引用解析与事件引用角色填充条款已随 legacy 压缩管线退役，行为由现行投影/装配路径承载。

## Requirements

> **符号对照（2026-11-03 归档核验）**：本文曾描述的四条契约中，引用解析角色推断、事件引用角色填充与错误日志区分度所对应的实现符号已在 Go 树 0 定义，随 legacy 管线退役；其后继行为由现行投影与装配路径承载，不再保留为独立 Requirement。仍现行的去重契约以 `ReplayProjection.Append` 为载体（`memory/replay/replay_budget.go`）。

### Requirement: 投影追加按事件键幂等

`ReplayProjection.Append` SHALL 对已投影过的条目 key 跳过追加并保持既有条目不变：同一事实第二次进入投影 MUST NOT 产生第二份条目。无 key（空 EventKey）的条目 SHALL NOT 参与去重、总是追加。幂等判定 SHALL 以条目携带的 canonical key 为唯一依据，MUST NOT 读取消息内容或依赖调用方去重。

#### Scenario: 同键第二次追加被跳过

- **WHEN** Append 收到一个 key 已在投影内的条目
- **THEN** 该次追加 SHALL 被跳过，投影条目表与渲染结果不变

#### Scenario: 不同键正常追加

- **WHEN** Append 收到一个 key 不在投影内的条目
- **THEN** 该条目 SHALL 被追加，并参与后续渲染

#### Scenario: 无键条目不参与去重

- **WHEN** Append 收到一个空 key 的条目
- **THEN** 该条目 SHALL 总是被追加（无键可去重）
