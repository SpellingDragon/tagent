# evolution-evaluation Specification

## Purpose

后验评估能力:窗口锚=improvement 事件(register commit 时刻)、judge_delay 一次性快照、结论四态(健康/劣化/样本不足/未到期)、劣化只产 evaluation 建议事件;bundle/发布道体系退役。
## Requirements

### Requirement: 后验评估锚点(improvement 事件)
LLMJudge 与 MetricGuardrail 的评估窗口 SHALL 以 improvement 事件的时间戳(=登记 commit 时刻)为锚点——事件即窗口表,无独立时刻表组件;窗口证据(feedback join、治理拒绝率等)从锚点起算。judge 延迟窗口(`judge_delay`,原 canary_hold 语义)SHALL 保留。

#### Scenario: 窗口从登记时刻起算
- **WHEN** refine register 产生 commit S(improvement 事件 ts=T)且 judge_delay 到期触发评估
- **THEN** 评估证据收集窗口为 [T, 评估时刻],feedback 经改进版本章(sha=S)精确 join——与 bundle 时代的 W4 语义等价

### Requirement: 建议式劣化信号(框架不动手)
guardrail/judge 判定劣化时,系统 SHALL 将结论与回滚建议写为 evaluation 事件(Content 含窗口 sha、verdict、证据摘要、建议 `refine rollback <sha>` 文案与「先 diff」引导),经既有事件消费面(冥想 digest 钩子/召回)渗透给 agent;MUST NOT 自动执行任何 git 回滚操作,亦 MUST NOT 经消息注入通道打断——执行权永远在 agent/人。

#### Scenario: 硬指标劣化仅建议
- **WHEN** 某窗口负反馈率超阈(guardrail Breach)
- **THEN** evaluation 事件落库(含证据数字与 sha);agent 在下一反思点经冥想 digest 看到建议,自行决定 rollback、diff 复核或保留观察;框架不执行 revert

#### Scenario: LLM-judge 劣化仅建议
- **WHEN** judge 评估窗口证据判定劣化
- **THEN** 同上——evaluation 事件渗透,无自动回滚

### Requirement: bundle 体系退役
系统 MUST 删除 BundleStore/Bundle/InitBaseline/VersionedSource/BundleProvider 与 ReleaseManager 发布状态机(Lane/Stage/Submit/快慢道/ProtectedPrompts/预算 Gate)及 refine 的 propose/diff 操作;evolution 配置段的发布道字段 MUST 同步删除。系统 MUST NOT 保留 bundle/git 双后端开关。

#### Scenario: evolution 启用下的提示词真源
- **WHEN** evolution 启用且 agent 修改 resources/prompts/ 下系统提示词文件
- **THEN** 改动经 mtime 热重载直接生效——无任何快照遮蔽层(旧 VersionedSource 遮蔽行为消失)

#### Scenario: 旧 bundle 存档
- **WHEN** 存量部署带有历史 bundle JSON 存档
- **THEN** 升级后存档保持只读原样(无自动迁移),运行不受影响;文档提供一行迁移注记

### Requirement: 改进版本章(feedback join 保持)
persistBusEvent 盖章的版本章来源 SHALL 从 active bundle 迁移为最新 improvement 事件的 sha(事件查询,无独立组件);无事件时不盖章(退化为时间窗 join,与现状无 active bundle 一致);BindFeedback 的章继承机制与键名(MetaKeyBundleID)MUST 保持不变。

#### Scenario: feedback 精确 join 改进版本
- **WHEN** 窗口 sha=S 开启后产生任务结算负反馈
- **THEN** 该 feedback 事件及因果绑定的子事件携带章 bundle_id=S,guardrail 窗口评估沿章精确归因
