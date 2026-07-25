# conversation-self-heal Specification

## Purpose
TBD - created by archiving change conversation-self-heal. Update Purpose after archive.
## Requirements
### Requirement: 投影按 EventKey 幂等追加

`SessionProjection` 追加事件引用时 SHALL 按 `EventKey` 幂等：当一个 `EventKey > 0` 的引用已存在于投影中时，重复追加 SHALL 被跳过（不产生第二份），并 SHALL 记录一条 warning 以便观测重复来源。`EventKey == 0`（未编号事件）SHALL NOT 参与去重。压缩替换（`Replace`）后，已见 key 的判定 SHALL 与替换后的投影一致。

#### Scenario: 同一事件重复投影被跳过

- **WHEN** 一个 `EventKey > 0` 的事件引用被追加两次
- **THEN** 投影中该 key SHALL 只保留一份
- **AND** 第二次追加 SHALL 被跳过并记录 warning

#### Scenario: 未编号事件不去重

- **WHEN** 追加多个 `EventKey == 0` 的引用
- **THEN** 它们 SHALL 全部保留（不因 key 相同而误合并）

