## ADDED Requirements

### Requirement: resume 任务链上下文还原器

sub agent 以 resume 方式重入时,框架 SHALL 自动还原任务链上下文并注入 `external_context`:本任务的前序轮次链（{指令, 结果} 对,上次 settle 结果为首,近 N 轮封顶）。当 settle 结果携带归档事件 key（resultRef 桥,后续演进）后,还原器 SHALL 可沿 RelationStore 因果链额外回溯固化物（段摘要优先）——注入接缝已预留,协议不变。还原器定位为 resume 专用机制,SHALL NOT 改变主 agent 装配与 recall 的既有路径（"子写、顶读、顶编排"不变:还原是框架代顶层做的工程化喂入,子 agent 仍为单 turn 原语）。

#### Scenario: 任务链还原只含相关内容

- **WHEN** sub agent 以 resume 方式重入任务 T
- **THEN** 注入的 external_context SHALL 包含 T 的上次 settle 结果与 T 的前序轮次
- **AND** SHALL NOT 包含无关任务的内容

#### Scenario: 无已结算轮次时拒绝重入

- **WHEN** 任务从未成功 settle 过即被 resume
- **THEN** SHALL 返回明确错误并引导（relaunch 或新调用）,不注入空上下文

### Requirement: 固化物因果链挂载

段摘要固化入库时 SHALL 通过 `RelationStore.SetParent` 挂载因果链（parent=段尾事件 key）,并保留来源 key 集合,使任务链回溯与 recall_trace 可达。

#### Scenario: 段摘要可沿链回溯

- **WHEN** 段 S（事件 k1..kn）被归档为摘要 s
- **THEN** `GetParent(s)` SHALL 返回 kn,且 s 的记录 SHALL 含 k1..kn 来源集合
