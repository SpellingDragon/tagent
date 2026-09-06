# feedback-binding 规格增量

## ADDED Requirements

### Requirement: feedback 事件类型
系统 MUST 经 EventTypeSpec 注册表注册 `feedback` 事件类型：正 key、Role=system、TTL 可配置（默认 30 天）、不进 LowValueEventTypes、Recallable=true。

#### Scenario: 注册即全链路生效
- **WHEN** `feedback` 在 event/registry.go init() 注册
- **THEN** lifecycle TTL、压缩定级、嵌入选择性、recall 过滤全链路一致生效，无需其他触点修改

### Requirement: 回执-反馈因果绑定
feedback 事件 MUST 经 RelationStore.SetParent 绑定到其评价的产出事件（parent），Content 为结构化 JSON（verdict/rating/note/source），零新索引。

#### Scenario: 任务结算自动反馈
- **WHEN** TaskManager settle 为 completed 或 failed
- **THEN** 对 spawn turn 的 agent_output 自动写入 task_settle 来源的 feedback 事件（completed→positive / failed→negative）；settle 为 suspect 时不写

#### Scenario: API 反馈
- **WHEN** 客户端 `POST /feedback`（event_key + verdict + note）
- **THEN** 写入 api 来源 feedback 事件并因果绑定该 event_key；event_key 不存在时返回显式错误

### Requirement: guardrail 负反馈判据
MetricGuardrail canary 窗口 MUST 聚合 `negative_feedback_rate`（经 feedback 因果边 join 到 parent 事件的 bundle_id 归因），阈值独立配置。

#### Scenario: 负反馈超阈回滚
- **WHEN** canary 窗口内某 bundle 的 negative_feedback_rate 超阈值
- **THEN** 触发该 bundle 的回滚（与既有两率判据并列）
