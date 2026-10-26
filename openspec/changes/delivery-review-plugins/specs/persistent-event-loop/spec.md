## MODIFIED Requirements

### Requirement: 循环面的模型感知边界

循环面的全部输入 SHALL 按三族分类，且只有前两族可作为说话素材：**用户世界**（用户消息，构成回合）、**业务需注意**（任务终态、具名拒绝、工具错误，作为行动与汇报素材）、**系统内部确认**（受理回执、投递成功回执、progress 状态、执行资源中间态，MUST NOT 成为说话素材）。系统内部确认 SHALL 只以最小确认回答其对应调用，具名拒绝 MUST NOT 被收敛成静默。三族的逐条语义与回执正文见 `quiet-receipts`，本要求只固定循环面的呈现边界，不另建同义要求。

progress 通知 SHALL 只作为其 range 的事实落链（通知身份与稳定序号沿用本能力「Handoff 未来通知的目标、实例与稳定序号」），MUST NOT 进入该 range 的 intake 呈现面，因而 MUST NOT 使下一步 decision 的模型请求增加任何消息；该通知本身 MUST NOT 被丢弃，其事实仍按身份可查。

失败极性 SHALL 达至用户面，而不止于事件流与日志：真失败且该回合没有可交付的助手正文时，用户 SHALL 收到携带**原始错误信息**的告知，MUST NOT 因“避免打扰”而静默结束回合。失败告知的触发条件、去重、与既有回执通道的关系及留痕口径见 `failure-notice`，本要求只固定“失败必须可达用户”这一条。

#### Scenario: 受理回执不构成模型材料

- **WHEN** 一次委派调用被受理，原 invocation 的下一步 decision 随即形成模型请求
- **THEN** 该请求 MUST NOT 含受理回执的任务号、状态词或结算承诺；受理回执只以最小确认回答该调用，其事实 MAY 落链供审计、MUST NOT 作为说话素材呈现

#### Scenario: progress 只落 fact 不进 intake

- **WHEN** 一个 range 在执行中收到同一任务的 progress 通知
- **THEN** 该通知 SHALL 作为事实落链并保留其通知身份与稳定序号，MUST NOT 进入该 range 的 intake 呈现面；下一步 decision 的模型请求 MUST NOT 因该 progress 增加任何消息，该通知 MUST NOT 被丢弃或覆盖；多 progress 的稳定序号与有序性沿用本能力「Handoff 未来通知的目标、实例与稳定序号」

#### Scenario: 失败回合必达用户告知

- **WHEN** 模型调用真失败（框架内重试已耗尽）且该回合没有可交付的助手正文
- **THEN** 用户 SHALL 收到恰好一条告知，其正文携带**原始错误信息**，由 sender 直投而非模型生成；同一事实 SHALL 落 fact 供审计、失败原因 SHALL 出现在日志中；成功回合 MUST NOT 出现该告知

#### Scenario: exec 中间态不构成模型材料、不驱动回合
- **WHEN** 执行资源管理器在调用返回后观察到执行资源的中间态（stable、suspect 或 exited）
- **THEN** 该观察 MUST NOT 驱动 follow-up 回合，模型请求数 MUST NOT 因此增加，且 SHALL 只落无载荷审计事实（无消息正文、无通知分类与任务号）、MUST NOT 作为说话素材或进入下一轮 decision 的模型请求

#### Scenario: 投递前审核是投递的必要条件
- **WHEN** 宿主把一条正文（回合正文、失败告知或跨代理 send 的正文）投递到目标会话
- **THEN** 该投递 SHALL 在到达渠道前把投递语义（正文、血统、目标）交审核插件裁决：allow/record SHALL 恰好投递一次；hold MUST NOT 投递、SHALL 挂起并在批准后原样补投恰好一次且 MUST NOT 被报告为投递；拒绝 MUST NOT 投递并 SHALL 留下无载荷审计事实（投递身份、血统、目标与字节数可查）；未配置审核时 SHALL 照常投递（默认 allow）
