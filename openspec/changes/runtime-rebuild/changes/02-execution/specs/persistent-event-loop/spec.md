# persistent-event-loop delta（02-execution）

> 复用登记（非规范，不改本条语义）：持久事件循环复用框架 model/tool 协议，**单一** channel/iterator 消费与单一终局归约，不引 LLMAgent 第二工具循环；业务 turn 从 state 借出**同代**绑定。
> 本 delta 改动「模型入口错误以失败极性呈现」「业务 turn 开始执行时绑定编排版本」「循环停止为终结态」「task_settled 作为一等事件触发回收 turn」（下一 decision 仍属原 invocation，不新造）并新增「Handoff 未来通知的目标、实例与稳定序号」；本能力其余 Requirement（Loop goroutine 持续运行不退出、批量 drain mailbox、mergeBatch 合并批量消息、InjectMessage 双模式、循环不依赖 bus echo 自触发、outputCh 宽限与溢出落盘、可判定的接收结果、可靠输入全序持久化、输入处理确认与幂等、LLM 端点重定向逐跳受 allowlist 约束、瞬时提交失败的重排覆盖完整冻结接收集、不可解码 slot 触发整封隔离、per-turn echo 凭据随 turn 清理统一回收、可靠模式的恢复能力与格式准入、启动直接核对未确认完成证据、inbox 写者唯一性与目录单属主）由主 spec `openspec/specs/persistent-event-loop/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 模型入口错误以失败极性呈现

回合执行中模型入口的任何失败——执行凭据校验拒绝、迭代器创建失败、流通道创建失败或返回 nil 流——MUST 以携带错误信息的失败 Response 在事件流中呈现，并使回合归约为 failed turn、形成 failed completion；MUST NOT 静默零产出使回合被归约为 completed、以 completed 口径冻结 completion 并 ack 持久输入。上游框架契约（迭代器创建失败可经 error 返回、流内错误编码进 Response.Error）SHALL 在所有模型包装层被同构兑现。

模型面 SHALL 以**单一** channel/iterator 消费并归约终局——有效终局、usage 尾包、partial-only、错误、取消、生产者结束（ProducerDone）分别取证；系统 MUST NOT 引入 LLMAgent 或任何第二工具迭代循环。终局 `unknown` MUST 告警，MUST NOT 默认 completed。usage 尾包 SHALL 在同一消费路径归约（见 `request-budget-accounting`）。

#### Scenario: 迭代器创建失败归约 failed turn

- **WHEN** 执行门包装的模型在迭代器创建时返回 error
- **THEN** 事件流收到带 Response.Error 的失败响应，回合归约 turnFailed 并形成 failed completion，持久输入不被以 completed 口径 ack

#### Scenario: nil 流不挂死不伪成功

- **WHEN** inner 模型返回 (nil, nil)
- **THEN** 同样以失败响应呈现并归约 failed turn，不永久阻塞、不归约 completed

#### Scenario: 凭据校验拒绝可见

- **WHEN** 执行凭据 verify 失败拒绝模型调用
- **THEN** 失败以带错误信息的失败响应进入事件流（含日志），回合失败可观测

#### Scenario: ProducerDone 与终局一致

- **WHEN** 流以生产者结束（ProducerDone）收尾且期间无错误
- **THEN** 归约 SHALL 恰好产生一个终局，MUST NOT 因 ProducerDone 额外产生第二个终局或第二循环

#### Scenario: 不引第二工具循环

- **WHEN** 一个回合内模型返回工具调用
- **THEN** 工具轮次 SHALL 由框架既有 ReAct 路径处理，系统 MUST NOT 自建第二套工具迭代循环

### Requirement: 业务 turn 开始执行时绑定编排版本

常驻循环 SHALL 在冻结批次确定后、进入业务 turn 且首次读取编排绑定之前完成一次版本获取，并置于传输重试循环之外；一次业务 turn 的多次模型迭代与工具轮次使用同一绑定。正在排队的输入不提前绑定；task_settled 等一等事件触发的新顶层 turn 按开始执行时选择当前有效版本，其来源标记仅作诊断，不用于恢复旧执行。

由在途执行派生、进入另一 agent 事件管线的输入 SHALL 随入队携带派生引用并在消费时沿用发起版本，不因排队改读最新 effective；同一管线内不相容输入（不同父请求或不同继承代）不得混入同一批次。

业务 turn 使用的模型/工具/插件绑定 SHALL 从 state **借出同代**绑定（见 01 生效 state 与 02.5），MUST NOT 在循环内自建第二份状态真源，也 MUST NOT 在调用中途读取全局最新 agent 表。同一 session 的**不同 invocation** MUST NOT 合批进同一次模型请求：各自独立决策、各自携带解析与去重身份。

#### Scenario: 排队的派生输入保持发起版本

- **WHEN** G1 执行中向 B 投递的委派输入仍在 B 的队列中等待，期间发布 G2
- **THEN** B 消费该输入时沿用 G1 的派生绑定；B 随后开始的独立新输入按 G2 处理，两者不混入同一批次

#### Scenario: 排队输入使用执行时版本

- **WHEN** 输入在 G1 期间入队，G2 发布后才开始执行
- **THEN** 该 turn 使用 G2，接收与冻结协议不变

#### Scenario: 重试继承同一版本

- **WHEN** 某 turn 的 RunFlow 传输失败进入重试，期间发布新版本
- **THEN** 重试仍使用该 turn 开始时获取的绑定，不中途切换

#### Scenario: task_settled 回流选当前代

- **WHEN** G1 期间派生的后台任务在 G2 发布后 settle 并回流为新 turn
- **THEN** 新 turn 使用 G2；原后台执行沿用其启动时绑定直至实际停止

#### Scenario: 借同代绑定不自建真源

- **WHEN** 一个业务 turn 需要模型/工具/插件
- **THEN** 其 SHALL 从 state 借出同代绑定，MUST NOT 在循环内构造第二份可写状态或读全局最新表

#### Scenario: 同 session 不同 invocation 不合批

- **WHEN** 同一 session 上有两个不同 invocation 的就绪输入
- **THEN** 二者 SHALL 各自独立决策、分别进模型请求，MUST NOT 合并为同一批模型请求

### Requirement: 循环停止为终结态

同一个 TagentAgent 的 StopLoop SHALL 为终结操作：再次 StartLoop SHALL 显式返回错误，不返回关闭通道，不创建第二个循环。需要重新运行时 SHALL 创建新 agent 并从事实链恢复。重复 Stop/Close SHALL 幂等；Start/Stop/Close 的并发状态转换 MUST 受同一生命周期同步机制保护。每次成功启动的输出通道 SHALL 恰关闭一次，循环异常退出也 SHALL 更新真实 active/terminated 状态。

循环关闭 SHALL 以**真实义务收束**为准：先拒绝新输入、结算已接受义务、轮询真实停止确认；MUST NOT 把 `cancel` 调用或队列取空当作完成。非合作 producer 返回的 `false` MUST NOT 被当作已结束；截止期限到达 SHALL 返回未收束原因（见 02.9）。

#### Scenario: Stop 后 Start 的往返

- **WHEN** 同实例 StopLoop 返回后再次 StartLoop
- **THEN** 返回明确终结态错误；新实例可正常恢复并启动

#### Scenario: 两轮完整往返后停止

- **WHEN** 分别创建两个实例执行 Start→Stop→Close
- **THEN** 两个实例各关闭自己的通道一次，重复停止不 panic

#### Scenario: 并发启动停止

- **WHEN** Start/Stop/Close 并发执行或 loop 异常退出
- **THEN** active 状态与真实消费者一致，无 WaitGroup 误用、双关闭或后台孤儿

#### Scenario: 先拒新再结算

- **WHEN** 循环开始关闭
- **THEN** 新输入 SHALL 被拒绝，已接受义务 SHALL 被结算，MUST NOT 在关闭后仍受理新输入

#### Scenario: 非合作 producer false 不伪结束

- **WHEN** 关闭轮询一个非合作 producer，其返回 `false` 表示尚未结束
- **THEN** 系统 MUST NOT 把该 `false` 当作已结束，SHALL 保持未收束直到真实停止确认或截止

#### Scenario: 截止报告未收束原因

- **WHEN** 关闭在截止期限到达时仍有未收束义务
- **THEN** 关闭 SHALL 返回未收束原因，MUST NOT 以成功完成宣称

### Requirement: task_settled 作为一等事件触发回收 turn

`task_settled` SHALL 作为一等事件进入持久事件循环。当一个后台 Task settle 时，其 `task_settled` 事件 SHALL 经 EventBus 进入循环，并像外部输入一样驱动**下一步 decision**；当循环空闲（阻塞在 Pull）时，`task_settled` SHALL 能唤醒它。

该下一步 decision SHALL 归属**原 invocation**（沿用原调用身份；CallID 按 decision 作用域保真，见 `event-metadata-contract`），MUST NOT 偷偷新建 invocation——仅应用**显式 Handoff** 时改投新目标/实例（见本 delta ADDED「Handoff 未来通知的目标、实例与稳定序号」）。原主 spec 中「触发一个**新顶层 turn**（回收 turn）」的表述改按此收窄：消费该通知的是**原 invocation 的下一步 decision**，不新造调用归属。

若一个 decision 正在进行，`task_settled` SHALL 排队至下一轮被消费，SHALL NOT 打断进行中的 decision。

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

## ADDED Requirements

### Requirement: Handoff 未来通知的目标、实例与稳定序号

显式 Handoff（转交）后，**未来**通知 SHALL 投递到**新接收 session 与新实例**，并记录转交关联（原目标 → 新目标）；**已开始**的通知 SHALL 保留其原目标与身份，MUST NOT 被追回、MUST NOT 因重试改投新实例。

通知身份 SHALL 至少含 `{TaskID, ExecutionRound, Category, Seq}`，其中 `Seq` 为**稳定序号**；同一通知的重复运输 MUST NOT 生成第二份事实（幂等），多次 progress 通知 SHALL 按稳定 `Seq` 有序、可对账。

**实例所有者的源/目标区分**：通知的实例所有者 SHALL 区分**源实例（source instance，任务发起所属实例）**与**目标实例（target instance，Handoff 后的接收实例）**：Handoff 前产生的通知归**源实例**身份，Handoff 后的未来通知归**目标实例**身份；同一通知 MUST NOT 被两端实例同时认领或重复生成事实。本处「实例」为与实例身份（`InstanceID`）一致的运行时单位，非主 spec `runtime-resource-ownership` 的存储资源 owner。

**关闭时未交付须保留**：任一实例关闭时，其**尚未交付**的通知/义务 MUST **保留并按可恢复路径承接**（交由资源 owner 或恢复材料），MUST NOT 因实例退役而直接丢弃（不得以 `removed` 直接丢弃）；未确认停止者沿用 `runtime-resource-ownership` 的 poisoned 保有规则。

#### Scenario: 未来通知转新目标与实例

- **WHEN** 一次显式 Handoff 后，原任务产生新的未来通知
- **THEN** 该通知 SHALL 投递到新接收 session 与新实例，并记录转交关联

#### Scenario: 已开始通知保留旧目标

- **WHEN** 某通知在 Handoff 前已开始投递
- **THEN** 该通知 SHALL 保留原目标/身份，MUST NOT 被追回或改投新实例

#### Scenario: 重试不改投

- **WHEN** 一次通知投递失败并重试
- **THEN** 重试 SHALL 投往同一（按 Handoff 时点确定的）目标，MUST NOT 中途改投

#### Scenario: 多 progress 稳定 seq 有序

- **WHEN** 同一任务在轮次内产生多条 progress 通知
- **THEN** 各通知的 `Seq` SHALL 稳定且有序，消费端可据此对账，重复运输不生成第二份

#### Scenario: 源/目标实例身份区分

- **WHEN** 一次 Handoff 发生，同一任务在 Handoff 前后各有通知
- **THEN** Handoff 前的通知 SHALL 归**源实例**身份，Handoff 后的未来通知 SHALL 归**目标实例**身份
- **AND** 同一通知 MUST NOT 被两端实例同时认领或重复生成事实

#### Scenario: 关闭未交付通知保留

- **WHEN** 一个实例关闭时而其仍有尚未交付的通知/义务
- **THEN** 未交付项 SHALL 保留并由可恢复路径承接（资源 owner / 恢复材料），MUST NOT 随实例退役直接丢弃（不以 `removed` 丢弃）
