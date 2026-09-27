# persistent-event-loop Delta

## ADDED Requirements

### Requirement: 瞬时提交失败的重排覆盖完整冻结接收集

持久循环在瞬时提交失败熬尽退避后重排 claim 时，SHALL 以本批的完整冻结接收集（received，含混批让位的 meditation 事件）为口径释放 claim，MUST NOT 只重排 selected 子集——让位事件的 durable envelope 若不被释放，将永久滞留 claimed 态（进程内无人再领取）直到重启。

#### Scenario: 混批中 meditation 让位后瞬时失败熬尽退避

- **WHEN** 一个批次同时包含用户输入与 meditation 事件（meditation 让位、仅用户输入被 selected），durable 提交遭遇瞬时故障并熬尽退避预算
- **THEN** 批内全部 durable claim（含让位 meditation 的 envelope）回到 pending，下一轮 Pull 按序重新领取，无 claim 滞留 claimed 态

### Requirement: 不可解码 slot 触发整封隔离

durable inbox 消费侧（claimDurable）恢复 envelope 的 source_event slot 失败时，SHALL 对该 envelope 整封执行隔离（QuarantineEnvelope，保留原始字节），MUST NOT 保留 claimed 等待重试（全坏形态跨重启死循环），也 MUST NOT 让同封可解码 slot 的 completion 导致整封 Ack（半坏形态静默销毁未执行输入）。

#### Scenario: envelope 全部 slot 不可解码

- **WHEN** 某 envelope 的每个 source_event 反序列化均失败
- **THEN** 该 envelope 被移入隔离区并携带原因，不留在 inbox 的 claimed/pending 态循环重试，隔离区内可查原始字节

#### Scenario: envelope 部分 slot 不可解码

- **WHEN** 某 envelope 的一个 slot 解码失败而其余 slot 可解码
- **THEN** 该 envelope 整封隔离（同封好 slot 不进入本批、不形成 completion、不触发 Ack），未执行输入不被销毁

### Requirement: per-turn echo 凭据随 turn 清理统一回收

事件循环安装的批次 echo spec（turnEcho）的作用域 SHALL 在 turn 的每一条退路上终结——其清理 MUST 与 turn 租约释放共享同一 per-turn 清理闭包（endTurn），MUST NOT 依赖成功路径的末尾清理点单独执行。

#### Scenario: 重试循环内早退路径

- **WHEN** turn 在重试循环内因 ctx 取消或执行代关闭而 turnStop 早退
- **THEN** turnEcho 已被置空，同一 ContextManager 上后续任何批次不会命中陈旧 echo spec 而跳过本应存储的用户输入
