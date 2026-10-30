# persistent-event-loop（消费语义增补）

## ADDED Requirements

### Requirement: 一次消费只看已到达的事件

一次模型消费的输入 SHALL 由本会话「已落链且未被此前消费取走」的事件组成，包含外部输入、工具结果（`action_command`）与通知材料；MUST NOT 要求它们同属一个 `InvocationID`。

#### Scenario: 跨来源合批
- **WHEN** 同一会话在取批时刻存在用户补充与一条已结算的工具结果，来源 invocation 不同
- **THEN** 两者 SHALL 进入同一次模型请求，并各自保留来源字段

#### Scenario: 未到达事件不进批
- **WHEN** 一条事件尚未落链（不在本批可见范围内）
- **THEN** 它 MUST NOT 出现在本批请求中，只可能进入后续批次

#### Scenario: 同一事件只消费一次
- **WHEN** 同一事件已被某一批消费
- **THEN** 后续批次 MUST NOT 再次携带它

#### Scenario: 显式隔离的事件单独成批
- **WHEN** 一条事件带有显式隔离标记（声明其需要独立上下文）
- **THEN** 它 SHALL 单独驱动一次消费，MUST NOT 与其他事件合批；隔离由事件声明决定，MUST NOT 由入口调用编号推定

### Requirement: 工具结果不阻塞下一次消费

工具调用派发后 SHALL NOT 同步等待其结果；receipts 进入当轮 transcript，结果作为后续批次事件进入下一次消费。

#### Scenario: 慢工具期间的新消息获得回答
- **WHEN** 上一轮已派发一个未返回的工具，用户在同一会话发出新消息
- **THEN** 新消息 SHALL 在工具返回前被消费并得到回答

#### Scenario: 本轮不含结果观察
- **WHEN** 一个工具尚未返回
- **THEN** 本轮 transcript MUST NOT 含该工具的结果观察；只有其 receipt 出现在当轮

### Requirement: 一次请求只看冻结批次

一次模型请求的消息集合 SHALL 在构造时冻结；MUST NOT 在请求中途追加事件。

#### Scenario: 请求构造后不追加
- **WHEN** 请求已构造并开始模型调用，期间又有事件到达
- **THEN** 该事件 MUST NOT 进入本次请求，只进入后续批次

### Requirement: 消费关系随产出记录

每次模型产出的 `agent_output` SHALL 在事实 `Meta` 记录本次请求实际携带的输入键集合；训练记录的 `InputEvents` SHALL 与该集合同源。

#### Scenario: 输出记录输入键
- **WHEN** 一次决策产出 `agent_output`
- **THEN** 该事实 SHALL 携带本次请求携带的输入键，且与 `rl.DecisionRecord.InputEvents` 一致
