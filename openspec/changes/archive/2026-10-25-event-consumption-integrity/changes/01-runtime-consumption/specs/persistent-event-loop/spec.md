## MODIFIED Requirements

### Requirement: task_settled 作为一等事件触发回收 turn

`task_settled` SHALL 作为一等事件进入持久事件循环。当一个后台 Task settle 时，其 `task_settled` 事件 SHALL 经 EventBus 进入循环，并像外部输入一样驱动**下一步 decision**；当循环空闲（阻塞在 Pull）时，`task_settled` SHALL 能唤醒它。

该下一步 decision SHALL 归属**原 invocation**（沿用原调用身份；CallID 按 decision 作用域保真，见 `event-metadata-contract`），MUST NOT 偷偷新建 invocation——仅应用**显式 Handoff** 时改投新目标/实例（见本能力「Handoff 未来通知的目标、实例与稳定序号」）。原主 spec 中「触发一个**新顶层 turn**（回收 turn）」的表述改按此收窄：消费该通知的是**原 invocation 的下一步 decision**，不新造调用归属。

若一个 decision 正在进行，`task_settled` SHALL 排队至下一轮被消费，SHALL NOT 打断进行中的 decision。该排队 SHALL 保留通知本身及其未结义务；驱动器忙时 MUST NOT 丢弃、覆盖或跳过该通知，MUST 在后续轮次完成逻辑消费。

一次终态通知的输入材料 SHALL 只来自该通知已提交事实或有来源的有界视图；同一通知身份 MUST NOT 为同一 decision 重复生成用户输入事实，MUST NOT 触发第二次逻辑消费。正文可在会话历史中合法再次出现；重复判定 SHALL 依据通知身份、事实键、动作身份与因果归属，MUST NOT 以正文出现次数、文本相似度或正文前缀判定。

不同 `TaskID`、责任 invocation 或 session 的通知 SHALL 各自保留其责任归属、送达目标与消费位置；同一 session 不同 invocation 不合批的既有义务见本能力「业务 turn 开始执行时绑定编排版本」，本要求不重复定义；不同 session 的通知 MUST NOT 互相覆盖送达目标或消费位置。

#### Scenario: 空闲时任务完成唤醒循环

- **WHEN** 事件循环空闲（阻塞在 Pull）且一个后台任务 settle
- **THEN** `task_settled` 事件 SHALL 唤醒循环并驱动**原 invocation 的下一步 decision**

#### Scenario: decision 进行中任务完成则排队

- **WHEN** 一个 decision 正在执行时某后台任务 settle
- **THEN** `task_settled` SHALL 排队，SHALL NOT 打断当前 decision，SHALL 在下一轮被消费

#### Scenario: 后台结果不新建 invocation

- **WHEN** 一次后台任务结算并回流
- **THEN** 回流 SHALL 落在原 invocation 的下一步 decision 上，MUST NOT 偷偷创建新 invocation
- **AND** 仅应用显式 Handoff 时才改投新目标/实例

#### Scenario: 同一通知不重复生成输入

- **WHEN** 一条终态通知驱动原 invocation 的下一步 decision，且该通知的已提交事实携带正文
- **THEN** 同一通知身份 MUST NOT 为同一 decision 生成第二份用户输入事实，MUST NOT 触发第二次逻辑消费
- **AND** 该正文可在会话历史或模型请求中合法再次出现，MUST NOT 以正文出现次数判定重复
- **AND** 重复判定 SHALL 依据通知/事实/动作身份与因果归属

#### Scenario: 忙时通知保留待办

- **WHEN** 一个 decision 正在执行时到达终态通知，且该 decision 持续到下一轮
- **THEN** 该通知 SHALL 保留在既有 intake/mailbox 的待办中，MUST NOT 被丢弃或覆盖
- **AND** 该通知 SHALL 在下一轮被消费并驱动原 invocation 的下一步 decision

#### Scenario: 多任务多会话各自续转

- **WHEN** 同一宿主上两个不同 `TaskID` 或两个不同 session 各自产生终态通知
- **THEN** 每条通知 SHALL 各自驱动其责任 invocation 的下一步 decision
- **AND** 不合批沿用既有条款，MUST NOT 互相覆盖送达目标或消费位置

## ADDED Requirements

### Requirement: 通知消费位置连续且不跨空洞

同一 `(TaskID, Round, Kind)`（`Round` 即既有 `ExecutionRound`，`Kind` 即既有 `Category`）的通知 SHALL 按稳定 `Seq` 形成连续消费位置。消费方 SHALL 只在位置 k 的逻辑消费确认后推进到 k+1；位置 k 未确认时 MUST NOT 确认 k+1，MUST NOT 以“后续位置已处理”掩盖空洞。领取（claim）MUST NOT 被视为消费确认。恢复 SHALL 从最后一个连续确认的位置继续，MUST NOT 跳过未确认位置，MUST NOT 回退到已确认位置之前。本要求规定位置连续性；单条输入的领取、消费确认与清理语义沿用本能力「输入处理确认与幂等」，不另建 inbox 或完成回执格式。

#### Scenario: 空洞阻断后续确认

- **WHEN** 位置 k 尚未确认而位置 k+1 的通知已到达并被处理
- **THEN** k+1 的消费确认 SHALL 被阻断或挂起
- **AND** MUST NOT 在 k 未确认时把 k+1 记为已消费

#### Scenario: 领取不提前确认

- **WHEN** 消费方 claim 一条通知但尚未完成逻辑消费
- **THEN** 该通知的消费位置 MUST NOT 被记为已确认
- **AND** 其未确认状态 SHALL 在恢复后保持

#### Scenario: 恢复从最后确认位置继续

- **WHEN** 进程重启且存在已确认位置 k 与未确认位置 k+1
- **THEN** 恢复 SHALL 从 k 之后继续，并把 k+1 作为下一个待确认位置
- **AND** MUST NOT 跳过 k+1，MUST NOT 回退到 k 之前

### Requirement: 正式宿主缺必要端口或身份时具名拒绝

承载持久事件循环的正式宿主 SHALL 在缺少运行所必需的端口或身份时以具名错误拒绝，MUST NOT 以默认值静默降级：根宿主缺少外发 `MessageSender` 时，用户地址的 turn MUST 返回具名错误且 MUST NOT 进入模型调用；微信宿主缺少声明身份时，`newWechatApp` MUST 返回具名错误且 MUST NOT 以默认地址启动；CLI 宿主选择真实模型而缺少凭据时，`run` MUST 以具名错误失败且 MUST NOT 回退到 mock。测试 MUST NOT 通过手工补接线使正式入口通过。

#### Scenario: 根宿主缺 sender 具名拒绝

- **WHEN** 以 `Boot` 装配的宿主未注入 `MessageSender`，且驱动入口 agent 的用户地址 turn
- **THEN** `Host.Invoke` SHALL 返回 `ErrHostSenderMissing`
- **AND** MUST NOT 发出 provider 请求或静默 headless 执行

#### Scenario: 微信宿主缺声明身份具名拒绝

- **WHEN** `newWechatApp` 收到空的入口身份
- **THEN** 其 SHALL 返回 `ErrNoChatAddress`（经 `NewSessionRouter` 的显式校验）
- **AND** MUST NOT 以默认 agent 或默认 session 前缀启动

#### Scenario: CLI 宿主缺模型凭据具名拒绝

- **WHEN** `run` 选择非 mock 模型且 `-api-key-env` 指向的环境变量为空
- **THEN** `run` SHALL 以非零码退出，并在 stderr 输出含模型名与环境变量名的具名错误
- **AND** MUST NOT 回退到 mock 或发出模型请求
