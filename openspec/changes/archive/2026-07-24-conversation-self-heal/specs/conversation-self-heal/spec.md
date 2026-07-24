## ADDED Requirements

### Requirement: 投影按 EventKey 幂等追加

`SessionProjection` 追加事件引用时 SHALL 按 `EventKey` 幂等：当一个 `EventKey > 0` 的引用已存在于投影中时，重复追加 SHALL 被跳过（不产生第二份），并 SHALL 记录一条 warning 以便观测重复来源。`EventKey == 0`（未编号事件）SHALL NOT 参与去重。压缩替换（`Replace`）后，已见 key 的判定 SHALL 与替换后的投影一致。

#### Scenario: 同一事件重复投影被跳过

- **WHEN** 一个 `EventKey > 0` 的事件引用被追加两次
- **THEN** 投影中该 key SHALL 只保留一份
- **AND** 第二次追加 SHALL 被跳过并记录 warning

#### Scenario: 未编号事件不去重

- **WHEN** 追加多个 `EventKey == 0` 的引用
- **THEN** 它们 SHALL 全部保留（不因 key 相同而误合并）

### Requirement: 发送模型前校验 tool 配对

在将组装好的消息序列发送给模型之前（上下文压缩之后），系统 SHALL 校验 tool 配对合法性：每条 `role=tool` 消息 SHALL 有前序、匹配其 `tool_call_id` 的 assistant tool_call；SHALL NOT 存在重复的 `tool_call_id`；SHALL NOT 存在孤立的 tool 消息。校验 SHALL 作用于最终发送的消息列表。

#### Scenario: 合法序列原样通过

- **WHEN** 消息序列中每条 tool 结果都有匹配的前序 tool_call 且无重复
- **THEN** 校验 SHALL 通过，消息序列 SHALL NOT 被改动

#### Scenario: 检出畸形配对

- **WHEN** 消息序列存在重复 `tool_call_id` 或孤立 tool 消息
- **THEN** 校验 SHALL 判定为畸形并触发保守修复

### Requirement: 畸形消息保守修复（仅本次发送）

校验检出畸形时，系统 SHALL 保守修复后再发送，且修复 SHALL 仅作用于本次发送的消息列表、SHALL NOT 回写持久投影。重复 tool 消息（同 `tool_call_id`）SHALL 保留首个、移除其余；孤立 tool 消息 SHALL 通过补占位 tool_call 或成对移除使序列合法。每次修复 SHALL 记录 warning。

#### Scenario: 重复 tool 结果去重后发送

- **WHEN** 同一 `tool_call_id` 的 tool 结果出现多次
- **THEN** 发送的消息中该结果 SHALL 只保留一份
- **AND** 持久投影 SHALL NOT 因此被修改

#### Scenario: 孤立 tool 结果修复为合法配对

- **WHEN** 存在无前序 tool_call 的孤立 tool 消息
- **THEN** 发送的消息 SHALL 通过补占位或移除使其配对合法
