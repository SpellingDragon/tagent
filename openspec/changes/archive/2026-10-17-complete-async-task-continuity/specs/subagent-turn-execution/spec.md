## MODIFIED Requirements

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

### Requirement: 投递对账屏障的 booking 与投递期望一一配对

投递屏障 SHALL 以task id、producer round、round owner及冻结WaitFor配对真实等待；producer stream结束与本次等待满足 SHALL 分离记录。booking SHALL 在资源可能产信号前注册；dedup、gate拒绝、失败意图及已满足的内联结果撤销本次预登记。非终局观察、重复/旧轮信号、被receiver拒绝的投递 MUST NOT 释放新轮或其他任务的等待。有限去重 SHALL 以已确认seq区间压缩，不以无界SourceID集合增长。

#### Scenario: A先就绪后退出不扣掉B
- **WHEN** 同调用派生A和B，A ready已满足本次等待，之后A终局到达而B仍等待
- **THEN** A后续信号不得扣掉B的booking，调用继续等B

#### Scenario: 两个任务并发resume
- **WHEN** 不同task产生相同kind/seq的信号
- **THEN** 身份按task+round独立匹配，不能串账；拒收时保持原等待和重试

### Requirement: Sub-agent 保持并发隔离与调用契约不变

每个新的本地逻辑调用 SHALL 创建独立EventBus、私有SessionProjection与ContextManager，MUST NOT 共享可变压缩器、activeBus或临时输入袋。按原invocation附着或恢复 SHALL 复用该调用保存的私有状态，不创建新的goal或污染其它调用。一次RunFlow返回但仍有等待时，调用scope SHALL 保持；scope结束或服务明确handoff后才退役其receiver。

#### Scenario: 两次独立调用隔离
- **WHEN** 同一owner并发处理两个不同invocation
- **THEN** 输入、projection、等待与结果分别属于各自scope，不跨调用传播

#### Scenario: 原调用结果附着
- **WHEN** 按已保存invocation附着等待中的逻辑调用
- **THEN** 继续同一私有投影和等待，不用空CM重跑初始goal

## ADDED Requirements

### Requirement: 本地子调用保持逻辑目标直至等待收敛

本地子调用 SHALL 使用独立bus、私有projection和CM复用统一事件壳；每个业务turn仍执行一次完整RunFlow。模型回复或dense ACK MUST NOT 在其派生的等待尚未满足、对应结果尚未受理时被当作父任务完成。逻辑调用 SHALL 保存原goal、父任务地址、等待集合、输入处置、最近输出和结果ACK状态；owner Close仅挂起，原父任务的可靠取消或TTL退役才终止该scope。

#### Scenario: 子agent先派命令后回复等待
- **WHEN** 子agent在一次RunFlow中派生长命令并回复等待
- **THEN** 父委派task仍等待原子调用；命令结果先回该私有bus，由后续turn处理后才返回最终结果

#### Scenario: 无后续工作时正常结束
- **WHEN** 当前turn结束且全部真实等待和已受理输入已排空
- **THEN** 逻辑调用完成并缓存结果，父任务受理结果后才确认该缓存可释放

## REMOVED Requirements

### Requirement: Sub-agent 调用执行恰好一个 turn

**Reason**: 一次RunFlow结束不等于含子任务等待的逻辑调用结束。

**Migration**: 每个业务turn仍复用RunFlow，逻辑调用由统一事件壳与持久continuation承接。
