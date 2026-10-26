# quiet-receipts Specification

## Purpose

系统内部确认的静默语义：受理回执、投递成功回执与 progress 通知不再是模型可复述的说话素材，只以最小确认回答其对应调用。反向边界同样属于本能力：具名拒绝、审批扣留与任务终态通知必须原样保留——静默只针对确认，不针对业务结果。

## Requirements

### Requirement: 受理回执为最小确认

系统 SHALL 在后台任务被受理、向模型回填该次调用的 tool 结果时，正文恰为 `ok`。该结果 MUST 保持 `tool` 角色并携带本次调用的 id（模型据此判定"调用已发生、结果稍后到"），但 MUST NOT 含 task 标识、执行状态词（如 `running`）、结算承诺（如 `will arrive as a notification`）或轮询提示。系统 MUST NOT 修改受理分支（新受理 / 审批扣留 / 其它既有分支）的判定语义。

#### Scenario: 回执正文恰为 ok

- **WHEN** 一次委派调用被受理，运行时把受理结果回填给模型
- **THEN** 该 tool 结果的正文 MUST 逐字符等于 `ok`
- **AND** 该结果的角色 MUST 为 `tool`，`ToolID` MUST 等于本次调用的 id

#### Scenario: 回执不携带任务身份与承诺

- **WHEN** 模型发起下一次请求
- **THEN** 受理回执中 MUST NOT 出现 task id、`running`、`background task`、`notification`、`settles` 任一素材
- **AND** 该断言 MUST 直接读取模型真实收到的请求消息，而非日志或内部字段

#### Scenario: 审批扣留不是受理

- **WHEN** 调用因需审批被扣留（`HoldReceipt`）
- **THEN** 回填正文 MUST 保留原有业务措辞（含 `parked awaiting approval`）
- **AND** MUST NOT 因本能力被降为 `ok`

### Requirement: 投递成功回执为最小确认

系统 SHALL 在宿主 send 工具投递成功时，向模型返回恰为 `ok` 的结果。该结果 MUST NOT 含目标会话 id、投递通道、条数或任何运营身份信息——成功无需素材，目标身份不属于模型决策输入。

#### Scenario: 成功回执正文恰为 ok

- **WHEN** 模型调用 send 工具且宿主成功投递
- **THEN** 回填给模型的 tool 结果正文 MUST 逐字符等于 `ok`

#### Scenario: 成功回执不外泄目标身份

- **WHEN** 成功回执被回填并进入下一轮请求
- **THEN** 该正文 MUST NOT 出现目标会话 id、`delivered to` 或 `[send_ok]`
- **AND** 断言 MUST 走正式宿主工具装配，不得在测试内直调私有方法冒充消费者

### Requirement: 具名拒绝必须保留

系统 SHALL 保持 send 工具拒绝路径的 `[send_denied] <reason>` 形态，`<reason>` MUST 可读且具名。拒绝属于「业务需注意」，模型必须能据此改参、换路径或如实告知用户。系统 MUST NOT 把拒绝降为 `ok`，也 MUST NOT 删除、泛化或截断拒绝原因。

#### Scenario: 拒绝原因抵达模型

- **WHEN** send 工具因策略或参数被拒绝
- **THEN** tool 结果 MUST 以 `[send_denied] ` 开头，并附带具体原因
- **AND** 该结果 MUST NOT 等于 `ok`

#### Scenario: 成功与拒绝同轮并存

- **WHEN** 同一轮内既有成功投递又有被拒绝的投递
- **THEN** 成功者的回执 MUST 为 `ok`，拒绝者的回执 MUST 保留 `[send_denied] <reason>`
- **AND** 两条回执 MUST 各自配对自己的调用 id

#### Scenario: 声明面同步

- **WHEN** send 工具的声明/说明面向模型描述拒绝形态
- **THEN** 该说明 MUST 与实际的 `[send_denied]` 形态一致
- **AND** MUST NOT 因成功回执改为 `ok` 而删除拒绝形态说明

### Requirement: progress 仅落事实

系统 SHALL 在收到 progress 通知时提交通知事实（`NoticeFactKey`、feedback 类型、kind/task 元数据、Seq 有序、重复运输幂等）。系统 MUST NOT 把该通知停入待取队列（intake），也 MUST NOT 让它作为任何模型请求的输入材料。progress 的「静默」MUST NOT 被实现为「丢弃事实」：审计、投影与消费位置连续性仍 MUST 成立。

#### Scenario: 事实仍然提交

- **WHEN** 一条 progress 通知到达且 range 地址完整
- **THEN** 链上 MUST 出现且仅出现一份对应事实，kind 为 progress、task 标识正确
- **AND** 重复运输同一条通知 MUST NOT 生成第二份事实

#### Scenario: 下一请求不含 progress

- **WHEN** progress 通知被消费后模型发起下一轮请求
- **THEN** 请求消息中 MUST NOT 出现 progress 的轮次、状态或任务正文
- **AND** 待取队列 MUST 为空（无缓存通知被读入 transcript）

#### Scenario: 无归属地址时有意丢弃

- **WHEN** 通知的 range 地址不完整，无法归属事实
- **THEN** 系统 MUST NOT 提交事实，也 MUST NOT 把它送入模型
- **AND** 该丢弃 MUST NOT 影响同任务的终态通知投递

### Requirement: 静默不得波及终态通知

系统 SHALL 保持 `settled`/`failed`/`cancelled` 通知的既有投递路径与语义：提交终态事实、进入待取队列、驱动该 range 的下一决策。终态通知是「业务需注意」，MUST NOT 被本能力的任何静默规则收窄或跳过。

#### Scenario: 结算通知驱动跟进轮

- **WHEN** 一条已受理任务结算并发出 `settled` 通知
- **THEN** 该 range MUST 被驱动出下一决策轮，且模型 MUST 收到结算结果
- **AND** 结算正文 MUST 经正常投递路径抵达用户

#### Scenario: 失败与取消同样可达

- **WHEN** 任务以 `failed` 或 `cancelled` 通知结束
- **THEN** 该通知 MUST 与 `settled` 一样被提交事实并驱动下一决策
- **AND** MUST NOT 因「静默确认」被丢弃

#### Scenario: 静默边界可审计

- **WHEN** 审阅本能力的落点
- **THEN** 每条静默规则 MUST 能指出其**只**作用于受理回执、成功回执或 progress
- **AND** 任一改动若触及终态分支、`[send_denied]` 或审批扣留文案，MUST 判为越界
