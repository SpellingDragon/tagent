# telemetry-channel Delta

## ADDED Requirements

### Requirement: 遥测通知的通道身份与卡片形态

task_settled 通知 SHALL 以独立的遥测通道身份参与上下文装配，MUST NOT 以对话时间线成员形态（与用户输入同权的 external_input 全文消息）进入常驻投影。回收 turn 装配中每条通知以卡片呈现（状态+task_id+摘要+票据，≤300 chars）；failed 极性卡片行 SHALL 携带 ★（复用既有反思锚渲染）。详情正文 SHALL 经事实链+票据保持 recall 可达（发送侧 600 cap+spill 既有语义不变）。

#### Scenario: 心跳式交错下的回收 turn 装配

- **WHEN** 一条 task_settled 到达且其回收 turn 开始装配
- **THEN** 该通知以 ≤300 chars 卡片注入本轮（user 侧形态），其 22K/3K 详情不进 messages，票据 key 保留在卡片内
- **THEN** 任务运行状态由看板行呈现，状态查询不依赖时间线

#### Scenario: failed 通知的长期痕迹

- **WHEN** failed 极性的通知卡片生成
- **THEN** 卡片行携带 ★，随降级进入滚动综述后长期可检索；completed/stable 卡片无 ★

### Requirement: 消费状态决定遥测退出时点

遥测的退出单位 SHALL 为消费状态（客观可推导，零 LLM 参与）：已消费且已外显（回收 turn 有 agent_output 且经 outputCh 投递宿主）→ turn 收尾即时降级为票据行；已消费但内部性 → 保留一行摘要 N 轮后降级，N MUST 与 keepRecent 值同源（消费边界现读，无独立配置项）；未消费 → 保持完整直到被消费（MUST NOT 被丢弃或静默降级）。降级操作 SHALL 仅修改投影（事实链不可变红线不变）。连跑折叠（settle_fold）与消费降级 SHALL 幂等共存。

#### Scenario: 已外显通知即时退出

- **WHEN** 一条 settle 的回收 turn 产出 agent_output 并经 outputCh 投递宿主
- **THEN** 该 turn 收尾时通知降级为票据行，下一轮装配的常驻 messages 不含其全文

#### Scenario: 内部性通知的 N 轮提醒

- **WHEN** 一条内部巡检类 settle 被回收 turn 消费但输出未外显，keepRecent=2
- **THEN** 其一行摘要保留 2 个完整段后降级；热更 keepRecent 后 N 自动跟随

#### Scenario: 未消费通知不可丢

- **WHEN** 停机期间错过消费的 settle 在重启后进入投影
- **THEN** 其以完整形态保持直到被某回收 turn 消费；积压有界性由任务层 TTL reaper 与批量退役 N→1 汇总在源头保证，上下文层不设数量/体积上限

### Requirement: compaction 豁免未消费遥测

compaction 的 L3 预算升级 MUST NOT 归档含未消费遥测的段（跳过或先促成其消费）——防止积压峰值触线时未读事件被静默归档。

#### Scenario: 积压峰值触线

- **WHEN** 未消费 settle 存量较大且对话窗口溢出触发 compaction
- **THEN** 含未消费通知的段被 L3 豁免，压缩通过其他段完成或如实报告无法达标

### Requirement: 召回暂存的生命周期闭环

recall 取回的详情 SHALL 以标记来源的暂存 ref 进入投影，消费完成后按遥测降级规则退出——MUST NOT 无限期常驻（防召回洪水重新膨胀）。

#### Scenario: 召回往返

- **WHEN** 用户询问历史细节，agent recall 取回 9K 详情并回答
- **THEN** 回答所在 turn 收尾后该暂存 ref 降级（票据行），后续装配不再包含其全文

### Requirement: 跨重启消费状态重建

投影重建（WAL 回放）SHALL 按「settle 事件 → 其后回收 turn 的 agent_output → outputCh 投递记录」序列重导出每条遥测的消费状态，MUST NOT 新增独立持久化面。

#### Scenario: 重启后的状态一致性

- **WHEN** 进程重启并回放投影
- **THEN** 已消费通知重建为降级形态、未消费通知重建为完整形态，与停机前一致
