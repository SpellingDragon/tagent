# subagent-turn-execution Specification

## Purpose

定义本地与远端子agent委派的执行边界、私有上下文和任务交付语义。本地逻辑调用在同一事件壳内可跨多个RunFlow业务turn等待子任务；完成、恢复和显式续行分别保存自己的身份，不以中间回复或固定整调用deadline截断目标。
## Requirements
### Requirement: 最终响应识别区分 assistant 响应与工具结果

判断一个事件是否为最终响应(`isFinalResponse`)SHALL 同时要求消息角色为 `assistant` **且** 无 tool_calls。工具结果事件(`Role=tool`)即使无 tool_calls,也 SHALL NOT 被识别为最终响应。

#### Scenario: 工具结果不被误判为最终响应

- **WHEN** 一个事件的消息 `Role=tool`(工具执行结果),无 tool_calls
- **THEN** `isFinalResponse` 返回 false
- **AND** `RunFlow` 不因此向 EventBus 回发 `agent_output` 事件

#### Scenario: assistant 最终消息被正确识别

- **WHEN** 一个事件的消息 `Role=assistant` 且无 tool_calls
- **THEN** `isFinalResponse` 返回 true
- **AND** `RunFlow` 向 EventBus 回发一个 `agent_output` 事件

#### Scenario: 带 tool_calls 的 assistant 响应不是最终响应

- **WHEN** 一个事件的消息 `Role=assistant` 且包含至少一个 tool_call
- **THEN** `isFinalResponse` 返回 false

### Requirement: Sub-agent 保持并发隔离与调用契约不变

每个新的本地逻辑调用 SHALL 创建独立EventBus、私有SessionProjection与ContextManager，MUST NOT 共享可变压缩器、activeBus或临时输入袋。按原invocation附着或恢复 SHALL 复用该调用保存的私有状态，不创建新的goal或污染其它调用。一次RunFlow返回但仍有等待时，调用scope SHALL 保持；scope结束或服务明确handoff后才退役其receiver。

#### Scenario: 两次独立调用隔离
- **WHEN** 同一owner并发处理两个不同invocation
- **THEN** 输入、projection、等待与结果分别属于各自scope，不跨调用传播

#### Scenario: 原调用结果附着
- **WHEN** 按已保存invocation附着等待中的逻辑调用
- **THEN** 继续同一私有投影和等待，不用空CM重跑初始goal


### Requirement: Sub-agent 调用超时对多轮工作宽容

本地子调用 SHALL 不再套一个覆盖整次逻辑调用的固定600s截止；等待producer期间不消耗active模型turn预算。模型turn仍 SHALL 受自己的有限预算和max_tool_iterations限制，Task统一TTL仍是整个producer的绝对上限。远端A2A适配可保留有界传输deadline并明确其能力边界，MUST NOT 据此声称本地continuation可跨进程恢复远端进程。

#### Scenario: 本地等待超过600秒
- **WHEN** 本地子agent合法等待长任务，且任务TTL允许
- **THEN** 不因固定的整调用600秒timeout取消等待；独立模型turn仍受限

#### Scenario: 父任务到期
- **WHEN** 父task统一TTL到期并提交可靠取消/退役事实
- **THEN** 对应逻辑子调用按原父地址终止，不延伸成无限调用

### Requirement: 子 agent 调用可作为异步任务执行

子agent SHALL 默认通过任务层异步执行，可保留显式同步适配。准备child invocation、task_round begin与parent binding SHALL 先于实际Run；完成/失败及输出正文 SHALL 先提交task_round finish，再发父task信号。dense内逻辑调用完成时内联返回；越窗返回ACK，直到同一逻辑调用结果可受理才结算，MUST NOT 以中间回复当完成。

#### Scenario: 越窗后仍属于原调用
- **WHEN** 子调用dense期结束但仍有子任务等待
- **THEN** ACK携带父task id，后续原invocation结果结算原producer round而不另造调用

### Requirement: 子 agent 委派使用同代目标绑定

`AgentToolWrapper` 的同步调用、传输重试、受管异步与本地/A2A 适配 SHALL 使用发起请求所持有版本中的子调用目标与声明，MUST NOT 在调用中途读取全局最新 agent 表或可变 agent 配置。本地目标 SHALL 借用唯一 resident owner；`Run` 是协作边界适配：输入经统一事件入口进入该 owner 的处理管线，请求级上下文由公共处理器创建并接该 agent 自己的服务（含其 taskController），不能按陈旧 owner.config 先造上下文后补 lease，也不得为被调方另造单轮专用执行架构或对已存在 agent 再造完整壳。当前关联的同步结果按原语义返回发起 turn，不作为新顶层输入。

调用的 message/session 标识、metadata、外部上下文和投影 SHALL 保持在本次调用作用域；不得通过共享 activeBus、lastSessionID 或 pendingExternalEvents 临时传递而污染并发调用。原直接 Ingest API 的单次交接语义须同步保留，不新增持久消息路由协议。

后台派生调用脱离父取消上下文时 SHALL 保留同代执行绑定与来源 metadata；已结束任务的 Resume/Relaunch 若创建新的 Run，有发起者则继承其版本，无发起者则获取当前版本，再从任务所属 owner 的执行面解析目标。所选版无目标明确拒绝，不复活旧执行器；仍持 G1 的发起者不因 G2 删除目标而丢失合法 G1 绑定。

#### Scenario: 多级调用使用同版本不同 owner 视图

- **WHEN** A→B→C 正在 G1 调用，期间 G2 改变 B 的工具或 C 的模型
- **THEN** 三层保持 G1，并分别使用 G1 中 A/B/C 的执行描述；B 不误用 A 的直接工具表，后续独立调用才用 G2

#### Scenario: 去壳后同 owner 并发调用隔离

- **WHEN** 同一 resident B 接收两路不同 invocation/session/external_context 的并发调用
- **THEN** 各自投影、注入 keys、metadata 和输出只属于自身请求，收尾不改写另一调用的 bus／session；两路都使用 B 的正确资源与原 RunFlow

#### Scenario: 被调方仍是完整 tagent

- **WHEN** B 作为 A 的被调方处理输入，期间 B 以自己的任务管理器启动 C 或命令并越过首答
- **THEN** C 的结算先进入 B 的事件总线并由 B 处理，随后按原请求关联产生向 A 的输出；B 的 bus、任务域与资源不因首答结束被关闭或并入 A

#### Scenario: 进行中热更不改变子调用目标

- **WHEN** 父请求在 G1 开始（A 可调用 B），执行期间发布 G2（A 改为可调用 C）
- **THEN** 该请求的工具声明与实际子调用仍为 B；下一个开始的新请求调用 C

#### Scenario: 移除目标后的新调用被拒

- **WHEN** G2 移除了 agent B 后，模型依据新声明发起了对 B 的陈旧调用或显式重投
- **THEN** 调用被明确拒绝并说明目标不存在，不静默改投其他 agent 或复用旧执行器

### Requirement: 真实委派上下文穿透工具装饰层

冷启动、候选、回滚和子调用接线 SHALL 使用现有透明工具解包能力，将委派绑定到实际父调用的 projection；MUST NOT 因 OutputLimitTool 包裹而跳过，也不得把候选壳空投影或其他调用投影作为父上下文。构造候选或调用私有 wrapper 不得原地修改已发布共享 wrapper。

#### Scenario: 省略 event_keys 自动注入真实父上下文

- **WHEN** 配置了 event_keys 参数的委派工具经生产包装链被调用，模型未传 keys，实际父投影有可注入事件
- **THEN** 子 agent 实际收到按既有规则选出的上下文，不因外层 OutputLimitTool 而缺失；显式 keys 仍优先，空投影及不支持该参数的普通工具不被强行注入

#### Scenario: 热候选与并发子调用不混用投影

- **WHEN** G1 调用持有其父上下文，同时构建并发布 G2，且不同子调用各自有私有投影
- **THEN** G1 的实际上下文不被重绑，G2 使用正确常驻父上下文，各子调用的下一层委派读取其本调用投影，不读取候选壳或其他调用投影

### Requirement: 投递对账屏障的 booking 与投递期望一一配对

投递屏障 SHALL 以task id、producer round、round owner及冻结WaitFor配对真实等待；producer stream结束与本次等待满足 SHALL 分离记录。booking SHALL 在资源可能产信号前注册；dedup、gate拒绝、失败意图及已满足的内联结果撤销本次预登记。非终局观察、重复/旧轮信号、被receiver拒绝的投递 MUST NOT 释放新轮或其他任务的等待。有限去重 SHALL 以已确认seq区间压缩，不以无界SourceID集合增长。

#### Scenario: A先就绪后退出不扣掉B
- **WHEN** 同调用派生A和B，A ready已满足本次等待，之后A终局到达而B仍等待
- **THEN** A后续信号不得扣掉B的booking，调用继续等B

#### Scenario: 两个任务并发resume
- **WHEN** 不同task产生相同kind/seq的信号
- **THEN** 身份按task+round独立匹配，不能串账；拒收时保持原等待和重试

### Requirement: 本地子调用保持逻辑目标直至等待收敛

本地子调用 SHALL 使用独立bus、私有projection和CM复用统一事件壳；每个业务turn仍执行一次完整RunFlow。模型回复或dense ACK MUST NOT 在其派生的等待尚未满足、对应结果尚未受理时被当作父任务完成。逻辑调用 SHALL 保存原goal、父任务地址、等待集合、输入处置、最近输出和结果ACK状态；owner Close仅挂起，原父任务的可靠取消或TTL退役才终止该scope。

#### Scenario: 子agent先派命令后回复等待
- **WHEN** 子agent在一次RunFlow中派生长命令并回复等待
- **THEN** 父委派task仍等待原子调用；命令结果先回该私有bus，由后续turn处理后才返回最终结果

#### Scenario: 无后续工作时正常结束
- **WHEN** 当前turn结束且全部真实等待和已受理输入已排空
- **THEN** 逻辑调用完成并缓存结果，父任务受理结果后才确认该缓存可释放

