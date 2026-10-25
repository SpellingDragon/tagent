## ADDED Requirements

### Requirement: 循环面的模型感知边界

循环面的全部输入 SHALL 按三族分类，且只有前两族可作为说话素材：**用户世界**（用户消息，构成回合）、**业务需注意**（任务终态、具名拒绝、工具错误，作为行动与汇报素材）、**系统内部确认**（受理回执、投递成功回执、progress 状态，MUST NOT 成为说话素材）。系统内部确认 SHALL 只以最小确认回答其对应调用，具名拒绝 MUST NOT 被收敛成静默。三族的逐条语义与回执正文见 `quiet-receipts`，本要求只固定循环面的呈现边界，不另建同义要求。

progress 通知 SHALL 只作为其 range 的事实落链（通知身份与稳定序号沿用本能力「Handoff 未来通知的目标、实例与稳定序号」），MUST NOT 进入该 range 的 intake 呈现面，因而 MUST NOT 使下一步 decision 的模型请求增加任何消息；该通知本身 MUST NOT 被丢弃，其事实仍按身份可查。

失败极性 SHALL 达至用户面，而不止于事件流与日志：真失败且该回合没有可交付的助手正文时，用户 SHALL 收到携带**原始错误信息**的告知，MUST NOT 因“避免打扰”而静默结束回合。失败告知的触发条件、去重、与既有回执通道的关系及留痕口径见 `failure-notice`，本要求只固定“失败必须可达用户”这一条。

#### Scenario: 受理回执不构成模型材料

- **WHEN** 一次委派调用被受理，原 invocation 的下一步 decision 随即形成模型请求
- **THEN** 该请求 MUST NOT 含受理回执的任务号、状态词或结算承诺；受理回执只以最小确认回答该调用
- **AND** 受理事实 MAY 落链供审计，MUST NOT 作为说话素材呈现

#### Scenario: progress 只落 fact 不进 intake

- **WHEN** 一个 range 在执行中收到同一任务的 progress 通知
- **THEN** 该通知 SHALL 作为事实落链并保留其通知身份与稳定序号，MUST NOT 进入该 range 的 intake 呈现面
- **AND** 下一步 decision 的模型请求 MUST NOT 因该 progress 增加任何消息，该通知 MUST NOT 被丢弃或覆盖
- **AND** 多 progress 的稳定序号与有序性沿用本能力「Handoff 未来通知的目标、实例与稳定序号」

#### Scenario: 失败回合必达用户告知

- **WHEN** 模型调用真失败（框架内重试已耗尽）且该回合没有可交付的助手正文
- **THEN** 用户 SHALL 收到恰好一条告知，其正文携带**原始错误信息**，由 sender 直投而非模型生成
- **AND** 同一事实 SHALL 落 fact 供审计，失败原因 SHALL 出现在日志中
- **AND** 成功回合 MUST NOT 出现该告知
